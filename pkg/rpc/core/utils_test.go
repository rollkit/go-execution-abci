package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTruncateNodeID(t *testing.T) {
	// Test case 1: Valid 32-byte hex string (SHA256 hash)
	t.Run("valid 32-byte hash", func(t *testing.T) {
		// Generate a 32-byte hash
		data := []byte("this is some data to hash for the test")
		hash := sha256.Sum256(data)
		fullNodeIDStr := hex.EncodeToString(hash[:]) // 64 chars

		expectedPrefix := fullNodeIDStr[:NodeIDByteLength*2] // First 20 bytes -> 40 chars

		truncatedNodeID, err := TruncateNodeID(fullNodeIDStr)
		assert.NoError(t, err, "TruncateNodeID should not return an error for a valid ID")
		assert.Equal(t, NodeIDByteLength*2, len(truncatedNodeID), fmt.Sprintf("Truncated Node ID should be %d characters long", NodeIDByteLength*2))
		assert.Equal(t, expectedPrefix, truncatedNodeID, "Truncated Node ID should be the first 20 bytes of the original ID")
	})

	// Test case 2: Node ID too short
	t.Run("node ID too short", func(t *testing.T) {
		shortNodeIDStr := "aabbccddeeff00112233445566778899" // 19 bytes -> 38 chars
		_, err := TruncateNodeID(shortNodeIDStr)
		assert.Error(t, err, "TruncateNodeID should return an error for a short ID")
		assert.Contains(t, err.Error(), "node ID too short", "Error message should indicate the ID is too short")
	})

	// Test case 3: Invalid hex string
	t.Run("invalid hex string", func(t *testing.T) {
		invalidHexStr := "not-a-hex-string-at-all-and-should-be-long-enough-to-pass-length-check"
		// Ensure it would be long enough if it were hex
		if len(invalidHexStr) < NodeIDByteLength*2 {
			invalidHexStr = strings.Repeat("x", NodeIDByteLength*2) // Make it long enough but still invalid hex
		}
		_, err := TruncateNodeID(invalidHexStr)
		assert.Error(t, err, "TruncateNodeID should return an error for an invalid hex string")
		assert.Contains(t, err.Error(), "failed to decode node ID", "Error message should indicate hex decoding failure")
	})

	// Test case 4: Empty string
	t.Run("empty string", func(t *testing.T) {
		_, err := TruncateNodeID("")
		assert.Error(t, err, "TruncateNodeID should return an error for an empty string")
	})

	// Test case 5: Node ID with exact required length (20 bytes)
	t.Run("exact length node ID", func(t *testing.T) {
		exactLengthNodeIDStr := strings.Repeat("a", NodeIDByteLength*2) // 40 chars
		truncatedNodeID, err := TruncateNodeID(exactLengthNodeIDStr)
		assert.NoError(t, err, "TruncateNodeID should not return an error for an ID of exact length")
		assert.Equal(t, exactLengthNodeIDStr, truncatedNodeID, "Truncated Node ID should be the same as input for exact length")
	})
}
