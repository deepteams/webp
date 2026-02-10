//go:build testc

package bitio_test

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/deepteams/webp/internal/bitio"
	cbitio "github.com/deepteams/webp/testc/bitio"
)

// TestBoolWriterMatch verifies that Go BoolWriter produces identical output
// to C VP8BitWriter for 1000 random sequences of 100 bits each.
func TestBoolWriterMatch(t *testing.T) {
	rng := rand.New(rand.NewSource(42))

	for seq := 0; seq < 1000; seq++ {
		const count = 100
		bits := make([]int, count)
		probs := make([]int, count)

		for i := 0; i < count; i++ {
			bits[i] = rng.Intn(2)
			probs[i] = rng.Intn(255) + 1 // 1..255
		}

		// Go encode
		bw := bitio.NewBoolWriter(0)
		for i := 0; i < count; i++ {
			bw.PutBit(bits[i], probs[i])
		}
		goBytes := bw.Finish()

		// C encode
		cBytes := cbitio.CBoolWriteSequence(bits, probs)

		if !bytes.Equal(goBytes, cBytes) {
			t.Fatalf("seq %d: Go output (%d bytes) != C output (%d bytes)\nGo: %x\nC:  %x",
				seq, len(goBytes), len(cBytes), goBytes, cBytes)
		}
	}
}

// TestBoolRoundTrip tests cross-library read/write compatibility:
//   - Go write -> C read
//   - C write -> Go read
func TestBoolRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(99))

	for seq := 0; seq < 500; seq++ {
		count := 50 + rng.Intn(100)
		bits := make([]int, count)
		probs := make([]int, count)

		for i := 0; i < count; i++ {
			bits[i] = rng.Intn(2)
			probs[i] = rng.Intn(255) + 1
		}

		// Go write -> C read
		bw := bitio.NewBoolWriter(0)
		for i := 0; i < count; i++ {
			bw.PutBit(bits[i], probs[i])
		}
		goBytes := bw.Finish()

		cReadBits := cbitio.CBoolReadSequence(goBytes, probs)
		for i := 0; i < count; i++ {
			if cReadBits[i] != bits[i] {
				t.Fatalf("seq %d Go->C: bit %d: got %d, want %d", seq, i, cReadBits[i], bits[i])
			}
		}

		// C write -> Go read
		cBytes := cbitio.CBoolWriteSequence(bits, probs)

		br := bitio.NewBoolReader(cBytes)
		for i := 0; i < count; i++ {
			got := br.GetBit(uint8(probs[i]))
			if got != bits[i] {
				t.Fatalf("seq %d C->Go: bit %d: got %d, want %d", seq, i, got, bits[i])
			}
		}
	}
}

// TestLosslessWriterMatch verifies that Go LosslessWriter produces identical
// output to C VP8LBitWriter for random sequences.
func TestLosslessWriterMatch(t *testing.T) {
	rng := rand.New(rand.NewSource(42))

	for seq := 0; seq < 1000; seq++ {
		count := 20 + rng.Intn(80)
		values := make([]uint32, count)
		nbits := make([]int, count)

		for i := 0; i < count; i++ {
			nbits[i] = rng.Intn(24) + 1 // 1..24
			mask := uint32((1 << uint(nbits[i])) - 1)
			values[i] = rng.Uint32() & mask
		}

		// Go encode
		bw := bitio.NewLosslessWriter(0)
		for i := 0; i < count; i++ {
			bw.WriteBits(values[i], nbits[i])
		}
		goBytes := bw.Finish()

		// C encode
		cBytes := cbitio.CLosslessWriteSequence(values, nbits)

		if !bytes.Equal(goBytes, cBytes) {
			t.Fatalf("seq %d: Go output (%d bytes) != C output (%d bytes)\nGo: %x\nC:  %x",
				seq, len(goBytes), len(cBytes), goBytes, cBytes)
		}
	}
}

// TestLosslessRoundTrip tests cross-library read/write compatibility:
//   - Go write -> C read
//   - C write -> Go read
func TestLosslessRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(99))

	for seq := 0; seq < 500; seq++ {
		count := 10 + rng.Intn(50)
		values := make([]uint32, count)
		nbits := make([]int, count)

		for i := 0; i < count; i++ {
			nbits[i] = rng.Intn(24) + 1
			mask := uint32((1 << uint(nbits[i])) - 1)
			values[i] = rng.Uint32() & mask
		}

		// Go write -> C read
		bw := bitio.NewLosslessWriter(0)
		for i := 0; i < count; i++ {
			bw.WriteBits(values[i], nbits[i])
		}
		goBytes := bw.Finish()

		cReadValues := cbitio.CLosslessReadSequence(goBytes, nbits)
		for i := 0; i < count; i++ {
			if cReadValues[i] != values[i] {
				t.Fatalf("seq %d Go->C: value %d: got %d (nbits=%d), want %d",
					seq, i, cReadValues[i], nbits[i], values[i])
			}
		}

		// C write -> Go read
		cBytes := cbitio.CLosslessWriteSequence(values, nbits)

		br := bitio.NewLosslessReader(cBytes)
		for i := 0; i < count; i++ {
			got := br.ReadBits(nbits[i])
			if got != values[i] {
				t.Fatalf("seq %d C->Go: value %d: got %d (nbits=%d), want %d",
					seq, i, got, nbits[i], values[i])
			}
		}
	}
}
