package main

import (
	"fmt"
	"log"

	cpu "github.com/drewwalton19216801/sixty502"
)

const ramSize = 0x10000 // 64KiB

// MainBus connects the CPU to RAM and other devices.
type MainBus struct {
	ram    [ramSize]uint8
	serial *SerialDevice
	// Add other devices here (PPU, APU, Controllers for NES)
}

// NewBus creates a new Bus with RAM and the Serial device.
func NewBus(serial *SerialDevice) *MainBus {
	return &MainBus{
		serial: serial,
		// ram is zero-initialized by Go
	}
}

// Read data from the bus. Routes to RAM or devices.
func (b *MainBus) Read(addr uint16) uint8 {
	switch {
	case addr == b.serial.addr:
		return b.serial.Read(addr)
	case addr < ramSize-1:
		return b.ram[addr]
	default:
		// Handle reads from unmapped memory, often returns 0 or last bus value
		// log.Printf("Warning: Read from unmapped address $%04X", addr)
		return 0
	}
}

// Write data to the bus. Routes to RAM or devices.
func (b *MainBus) Write(addr uint16, data uint8) {
	switch {
	case addr == b.serial.addr:
		b.serial.Write(addr, data)
	case addr < ramSize-1:
		b.ram[addr] = data
	default:
		// Handle writes to unmapped memory (often ignored)
		log.Printf("Warning: Write to unmapped address $%04X", addr)
	}
}

// Helper to load a program into RAM at a specific address
func (b *MainBus) LoadProgram(startAddr uint16, program []uint8) error {
	if int(startAddr)+len(program) > ramSize {
		return fmt.Errorf("program exceeds RAM bounds (start: $%04X, size: %d)", startAddr, len(program))
	}
	for i, byt := range program {
		b.Write(startAddr+uint16(i), byt)
	}
	log.Printf("Loaded %d bytes starting at $%04X", len(program), startAddr)
	return nil
}

// Helper to read a range of memory (for display)
func (b *MainBus) ReadRange(startAddr uint16, count int) []uint8 {
	data := make([]uint8, count)
	maxAddr := uint16(ramSize - 1)
	for i := 0; i < count; i++ {
		currentAddr := startAddr + uint16(i)
		// Handle wrapping or stopping at the end
		if currentAddr < startAddr && startAddr != 0 { // Wrapped around
			break // Stop reading if address wraps
		}
		if currentAddr > maxAddr {
			break // Stop if exceeding RAM size
		}
		data[i] = b.Read(currentAddr)
	}
	return data
}

// Ensure MainBus implements the cpu6502.Bus interface
var _ cpu.Bus = (*MainBus)(nil)
