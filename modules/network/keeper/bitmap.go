package keeper

import (
	"math/bits"
)

// BitmapHelper provides utility functions for bitmap operations
type BitmapHelper struct{}

// NewBitmapHelper creates a new bitmap helper
func NewBitmapHelper() *BitmapHelper {
	return &BitmapHelper{}
}

// NewBitmap creates a new bitmap for n validators
func (bh *BitmapHelper) NewBitmap(n int) []byte {
	size := (n + 7) / 8
	return make([]byte, size)
}

// SetBit sets the bit at index to 1
func (bh *BitmapHelper) SetBit(bitmap []byte, index int) {
	if index < 0 || index >= len(bitmap)*8 {
		return
	}
	byteIndex := index / 8
	bitIndex := uint(index % 8)
	bitmap[byteIndex] |= 1 << bitIndex
}

// IsSet checks if the bit at index is set
func (bh *BitmapHelper) IsSet(bitmap []byte, index int) bool {
	if index < 0 || index >= len(bitmap)*8 {
		return false
	}
	byteIndex := index / 8
	bitIndex := uint(index % 8)
	return (bitmap[byteIndex] & (1 << bitIndex)) != 0
}

// PopCount returns the number of set bits
func (bh *BitmapHelper) PopCount(bitmap []byte) int {
	count := 0
	for _, b := range bitmap {
		count += bits.OnesCount8(b)
	}
	return count
}

// OR performs bitwise OR of two bitmaps
func (bh *BitmapHelper) OR(dst, src []byte) {
	minLen := len(dst)
	if len(src) < minLen {
		minLen = len(src)
	}
	for i := 0; i < minLen; i++ {
		dst[i] |= src[i]
	}
}

// AND performs bitwise AND of two bitmaps
func (bh *BitmapHelper) AND(dst, src []byte) {
	minLen := len(dst)
	if len(src) < minLen {
		minLen = len(src)
	}
	for i := 0; i < minLen; i++ {
		dst[i] &= src[i]
	}
}

// Copy creates a copy of the bitmap
func (bh *BitmapHelper) Copy(bitmap []byte) []byte {
	if bitmap == nil {
		return nil
	}
	cp := make([]byte, len(bitmap))
	copy(cp, bitmap)
	return cp
}

// Clear sets all bits to 0
func (bh *BitmapHelper) Clear(bitmap []byte) {
	for i := range bitmap {
		bitmap[i] = 0
	}
}

// CountInRange counts set bits in a range [start, end)
func (bh *BitmapHelper) CountInRange(bitmap []byte, start, end int) int {
	count := 0
	for i := start; i < end && i < len(bitmap)*8; i++ {
		if bh.IsSet(bitmap, i) {
			count++
		}
	}
	return count
}
