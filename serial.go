package main

import (
	"strings"
	"sync"
)

// SerialDevice simulates a simple write-only serial port at a specific address.
type SerialDevice struct {
	mu     sync.RWMutex
	buffer strings.Builder
	addr   uint16
}

// NewSerialDevice creates a new serial device listening on the given address.
func NewSerialDevice(address uint16) *SerialDevice {
	return &SerialDevice{
		addr: address,
	}
}

// Write handles writes to the serial device's memory address.
func (s *SerialDevice) Write(addr uint16, data uint8) {
	if addr == s.addr {
		s.mu.Lock()
		s.buffer.WriteByte(data)
		s.mu.Unlock()
	}
	// Ignore writes to other addresses (shouldn't happen if bus routes correctly)
}

// Read handles reads from the serial device's memory address.
// For a simple output device, reads typically return 0 or a status byte.
func (s *SerialDevice) Read(addr uint16) uint8 {
	if addr == s.addr {
		// Maybe return a status like 0xFF if ready? For now, just 0.
		return 0
	}
	// Ignore reads from other addresses
	return 0
}

// GetOutput returns the current content of the serial buffer.
func (s *SerialDevice) GetOutput() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.buffer.String()
}

// ClearOutput resets the serial buffer.
func (s *SerialDevice) ClearOutput() {
	s.mu.Lock()
	s.buffer.Reset()
	s.mu.Unlock()
}
