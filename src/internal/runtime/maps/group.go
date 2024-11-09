// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package maps

import (
	"internal/abi"
	"internal/goarch"
	"internal/runtime/sys"
	"unsafe"
)

const (
	// Maximum load factor prior to growing.
	//
	// 7/8 is the same load factor used by Abseil, but Abseil defaults to
	// 16 slots per group, so they get two empty slots vs our one empty
	// slot. We may want to reevaluate if this is best for us.
	maxAvgGroupLoad = 7

	ctrlEmpty   ctrl = 0b10000000
	ctrlDeleted ctrl = 0b11111110

	bitsetLSB     = 0x0101010101010101
	bitsetMSB     = 0x8080808080808080
	bitsetEmpty   = bitsetLSB * uint64(ctrlEmpty)
	bitsetDeleted = bitsetLSB * uint64(ctrlDeleted)
)

// bitset represents a set of slots within a group.
//
// The underlying representation uses one byte per slot, where each byte is
// either 0x80 if the slot is part of the set or 0x00 otherwise. This makes it
// convenient to calculate for an entire group at once (e.g. see matchEmpty).
type bitset uint64

// first assumes that only the MSB of each control byte can be set (e.g. bitset
// is the result of matchEmpty or similar) and returns the relative index of the
// first control byte in the group that has the MSB set.
//
// Returns abi.SwissMapGroupSlots if the bitset is empty.
func (b bitset) first() uintptr {
	return uintptr(sys.TrailingZeros64(uint64(b))) >> 3
}

// removeFirst removes the first set bit (that is, resets the least significant set bit to 0).
func (b bitset) removeFirst() bitset {
	return b & (b - 1)
}

// Each slot in the hash table has a control byte which can have one of three
// states: empty, deleted, and full. They have the following bit patterns:
//
//	  empty: 1 0 0 0 0 0 0 0
//	deleted: 1 1 1 1 1 1 1 0
//	   full: 0 h h h h h h h  // h represents the H1 hash bits
//
// TODO(prattmic): Consider inverting the top bit so that the zero value is empty.
type ctrl uint8

// ctrlGroup is a fixed size array of abi.SwissMapGroupSlots control bytes
// stored in a uint64.
type ctrlGroup uint64

// get returns the i-th control byte.
func (g *ctrlGroup) get(i uintptr) ctrl {
	if goarch.BigEndian {
		return *(*ctrl)(unsafe.Add(unsafe.Pointer(g), 7-i))
	}
	return *(*ctrl)(unsafe.Add(unsafe.Pointer(g), i))
}

// set sets the i-th control byte.
func (g *ctrlGroup) set(i uintptr, c ctrl) {
	if goarch.BigEndian {
		*(*ctrl)(unsafe.Add(unsafe.Pointer(g), 7-i)) = c
		return
	}
	*(*ctrl)(unsafe.Add(unsafe.Pointer(g), i)) = c
}

// setEmpty sets all the control bytes to empty.
func (g *ctrlGroup) setEmpty() {
	*g = ctrlGroup(bitsetEmpty)
}

// matchH2 returns the set of slots which are full and for which the 7-bit hash
// matches the given value. May return false positives.
func (g ctrlGroup) matchH2(h uintptr) bitset {
	// NB: This generic matching routine produces false positive matches when
	// h is 2^N and the control bytes have a seq of 2^N followed by 2^N+1. For
	// example: if ctrls==0x0302 and h=02, we'll compute v as 0x0100. When we
	// subtract off 0x0101 the first 2 bytes we'll become 0xffff and both be
	// considered matches of h. The false positive matches are not a problem,
	// just a rare inefficiency. Note that they only occur if there is a real
	// match and never occur on ctrlEmpty, or ctrlDeleted. The subsequent key
	// comparisons ensure that there is no correctness issue.
	v := uint64(g) ^ (bitsetLSB * uint64(h))
	return bitset(((v - bitsetLSB) &^ v) & bitsetMSB)
}

// matchEmpty returns the set of slots in the group that are empty.
func (g ctrlGroup) matchEmpty() bitset {
	// An empty slot is   1000 0000
	// A deleted slot is  1111 1110
	// A full slot is     0??? ????
	//
	// A slot is empty iff bit 7 is set and bit 1 is not. We could select any
	// of the other bits here (e.g. v << 1 would also work).
	v := uint64(g)
	return bitset((v &^ (v << 6)) & bitsetMSB)
}

// matchEmptyOrDeleted returns the set of slots in the group that are empty or
// deleted.
func (g ctrlGroup) matchEmptyOrDeleted() bitset {
	// An empty slot is  1000 0000
	// A deleted slot is 1111 1110
	// A full slot is    0??? ????
	//
	// A slot is empty or deleted iff bit 7 is set.
	v := uint64(g)
	return bitset(v & bitsetMSB)
}

// groupReference is a wrapper type representing a single slot group stored at
// data.
//
// A group holds abi.SwissMapGroupSlots slots (key/elem pairs) plus their
// control word.
type groupReference struct {
	// data points to the group, which is described by typ.Group and has
	// layout:
	//
	// type group struct {
	// 	ctrls ctrlGroup
	// 	slots [abi.SwissMapGroupSlots]slot
	// }
	//
	// type slot struct {
	// 	key  typ.Key
	// 	elem typ.Elem
	// }
	data unsafe.Pointer // data *typ.Group
}

const (
	ctrlGroupsSize   = unsafe.Sizeof(ctrlGroup(0))
	groupSlotsOffset = ctrlGroupsSize
)

// alignUp rounds n up to a multiple of a. a must be a power of 2.
func alignUp(n, a uintptr) uintptr {
	return (n + a - 1) &^ (a - 1)
}

// alignUpPow2 rounds n up to the next power of 2.
//
// Returns true if round up causes overflow.
func alignUpPow2(n uint64) (uint64, bool) {
	if n == 0 {
		return 0, false
	}
	v := (uint64(1) << sys.Len64(n-1))
	if v == 0 {
		return 0, true
	}
	return v, false
}

// ctrls returns the group control word.
func (g *groupReference) ctrls() *ctrlGroup {
	return (*ctrlGroup)(g.data)
}

// key returns a pointer to the key at index i.
func (g *groupReference) key(typ *abi.SwissMapType, i uintptr) unsafe.Pointer {
	offset := groupSlotsOffset + i*typ.SlotSize

	return unsafe.Pointer(uintptr(g.data) + offset)
}

// elem returns a pointer to the element at index i.
func (g *groupReference) elem(typ *abi.SwissMapType, i uintptr) unsafe.Pointer {
	offset := groupSlotsOffset + i*typ.SlotSize + typ.ElemOff

	return unsafe.Pointer(uintptr(g.data) + offset)
}

// groupsReference is a wrapper type describing an array of groups stored at
// data.
type groupsReference struct {
	// data points to an array of groups. See groupReference above for the
	// definition of group.
	data unsafe.Pointer // data *[length]typ.Group

	// lengthMask is the number of groups in data minus one (note that
	// length must be a power of two). This allows computing i%length
	// quickly using bitwise AND.
	lengthMask uint64

	// entryMask is the total number of slots in the groups minus one.
	entryMask uint64
}

// newGroups allocates a new array of length groups.
//
// Length must be a power of two.
func newGroups(typ *abi.SwissMapType, length uint64) groupsReference {
	return groupsReference{
		// TODO: make the length type the same throughout.
		data:       newarray(typ.Group, int(length)),
		lengthMask: length - 1,
		entryMask:  (length * abi.SwissMapGroupSlots) - 1,
	}
}

// group returns the group at index i.
func (g *groupsReference) group(typ *abi.SwissMapType, i uint64) groupReference {
	// TODO(prattmic): Do something here about truncation on cast to
	// uintptr on 32-bit systems?
	offset := uintptr(i) * typ.Group.Size_

	return groupReference{
		data: unsafe.Pointer(uintptr(g.data) + offset),
	}
}
