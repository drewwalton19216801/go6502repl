package main

import (
	"fmt"

	cpu "github.com/drewwalton19216801/sixty502"
)

// RAM represents the 64KB memory space and implements the cpu6502.Bus interface.
type RAM struct {
	mem [65536]uint8
}

// NewRAM creates a new RAM instance initialized to zeros.
func NewRAM() *RAM {
	return &RAM{} // Go initializes arrays to zero values
}

// Read implements the cpu6502.Bus interface.
func (r *RAM) Read(addr uint16) uint8 {
	// The 6502 has no memory protection, reads wrap around.
	// Accessing the array index handles this naturally for uint16.
	return r.mem[addr]
}

// Write implements the cpu6502.Bus interface.
func (r *RAM) Write(addr uint16, data uint8) {
	// The 6502 has no memory protection, writes wrap around.
	r.mem[addr] = data
}

// Load loads a byte slice into RAM at the specified start address.
func (r *RAM) Load(startAddr uint16, data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("no data to load")
	}
	endAddr := int(startAddr) + len(data) - 1
	if endAddr > 65535 {
		return fmt.Errorf("data exceeds memory bounds (ends at $%04X)", endAddr)
	}
	for i, b := range data {
		r.mem[startAddr+uint16(i)] = b
	}
	return nil
}

// Dump returns a formatted string of memory contents.
func (r *RAM) Dump(startAddr uint16, count int) string {
	if count <= 0 || count > 1024 { // Limit dump size
		count = 16
	}
	if int(startAddr)+count > 65536 {
		count = 65536 - int(startAddr)
		if count < 0 {
			count = 0
		}
	}

	var output string
	bytesPerLine := 16

	for i := 0; i < count; i++ {
		addr := startAddr + uint16(i)
		if i%bytesPerLine == 0 {
			if i > 0 {
				output += "\n"
			}
			output += fmt.Sprintf("$%04X: ", addr)
		}
		output += fmt.Sprintf("%02X ", r.mem[addr])
	}
	output += "\n"
	return output
}

// Ensure RAM implements the Bus interface
var _ cpu.Bus = (*RAM)(nil)
