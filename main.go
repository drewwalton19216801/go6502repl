package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	cpu "github.com/drewwalton19216801/sixty502" // Adjust import path if needed
)

const serialAddr uint16 = 0xF001
const defaultLoadAddr uint16 = 0x0200 // Common area for small programs

// --- Test Program ---
// Simple program to write "Hello World!\n" to serial output (0xF001)
// and then loop indefinitely.
// Org $0200
var helloWorldProgram = []uint8{
	// Start:
	0xA2, 0x00, // LDX #$00      ; X = 0 (index into message)
	// Loop:
	0xBD, 0x10, 0x02, // LDA Message,X ; Load character A = RAM[0x0210 + X]
	0x8D, 0x01, 0xF0, // STA $F001     ; Write character to serial
	0xE8,       // INX             ; Increment index
	0xE0, 0x0E, // CPX #$0E      ; Compare X to length (14 chars incl. null)
	0xD0, 0xF5, // BNE Loop      ; Branch back if not equal
	// Halt:
	0x4C, 0x0C, 0x02, // JMP Halt      ; Infinite loop jump to self (0x020C)
	// Message: (Starts at 0x0210)
	'H', 'e', 'l', 'l', 'o', ' ', 'W', 'o', 'r', 'l', 'd', '!', '\n', 0x00, // The message + null terminator
}

// --- Bubbletea Model ---

type Mode int

const (
	CommandMode Mode = iota
	RunningMode
)

type model struct {
	cpu    *cpu.CPU
	bus    *MainBus
	serial *SerialDevice

	textInput textinput.Model
	mode      Mode
	err       error
	message   string // General status messages

	width, height int

	// View state
	memViewAddr    uint16
	disassembly    map[uint16]string
	memViewContent string
	serialOutput   string

	// Style
	styleHelp      lipgloss.Style
	styleCPU       lipgloss.Style
	styleDisasm    lipgloss.Style
	styleMemory    lipgloss.Style
	styleSerial    lipgloss.Style
	styleInput     lipgloss.Style
	styleStatus    lipgloss.Style
	styleHighlight lipgloss.Style
}

type tickMsg time.Time

func initialModel() model {
	// --- Device Setup ---
	serialDev := NewSerialDevice(serialAddr)
	mainBus := NewBus(serialDev)
	cpuCore := cpu.NewCPU(mainBus)

	// --- Input Setup ---
	ti := textinput.New()
	ti.Placeholder = "Enter command (h for help)..."
	ti.Focus()
	ti.CharLimit = 156
	ti.Width = 50

	// --- Load Program ---
	err := mainBus.LoadProgram(defaultLoadAddr, helloWorldProgram)
	if err != nil {
		log.Fatalf("Failed to load program: %v", err) // Fatal on initial load fail
	}
	cpuCore.PC = defaultLoadAddr // Set PC to start of loaded program
	// cpuCore.Reset() // Alternatively, if program is at reset vector target

	m := model{
		cpu:            cpuCore,
		bus:            mainBus,
		serial:         serialDev,
		textInput:      ti,
		mode:           CommandMode,
		memViewAddr:    defaultLoadAddr, // Start memory view near code
		styleHelp:      lipgloss.NewStyle().Faint(true),
		styleCPU:       lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1),
		styleDisasm:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1),
		styleMemory:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1),
		styleSerial:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Foreground(lipgloss.Color("10")), // Green
		styleInput:     lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1),
		styleStatus:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1),
		styleHighlight: lipgloss.NewStyle().Background(lipgloss.Color("237")).Foreground(lipgloss.Color("15")), // Highlight PC line
	}

	m.updateViews() // Initial view rendering content
	return m
}

func (m model) Init() tea.Cmd {
	return textinput.Blink // Start the cursor blinking
}

// --- Update Logic ---

func tickCmd() tea.Cmd {
	return tea.Tick(time.Millisecond*1, func(t time.Time) tea.Msg { // Faster tick for smoother run?
		return tickMsg(t)
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.mode {
		case CommandMode:
			switch msg.Type {
			case tea.KeyEnter:
				input := m.textInput.Value()
				m.textInput.SetValue("") // Clear input
				m.err = nil              // Clear previous error
				m.message = ""           // Clear previous message
				cmds = append(cmds, m.handleCommand(input))
				m.updateViews() // Update display after command

			case tea.KeyCtrlC, tea.KeyEsc:
				return m, tea.Quit

			default:
				// Let the text input handle the key press
				m.textInput, cmd = m.textInput.Update(msg)
				cmds = append(cmds, cmd)
			}

		case RunningMode:
			switch msg.Type {
			case tea.KeyCtrlC, tea.KeyEsc:
				m.mode = CommandMode // Pause execution
				m.message = "Execution paused."
				m.textInput.Focus() // Focus input field when pausing
				cmds = append(cmds, textinput.Blink)
				m.updateViews()
			}
		}

	case tickMsg:
		if m.mode == RunningMode {
			// Execute one CPU clock cycle
			m.cpu.Clock()
			// Continue ticking only if Cycles > 0 or if instruction finished
			if m.cpu.Cycles == 0 {
				// Instruction finished, update views and schedule next instruction fetch/exec
				m.updateViews() // Update potentially slow things less often
			}
			// Keep ticking to finish current instruction or start next
			cmds = append(cmds, tickCmd())
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Re-calculate layout dimensions if needed here
		m.updateViews() // Update views with new size info

	case error: // Handle errors passed as messages
		m.err = msg
		m.message = fmt.Sprintf("Error: %v", msg) // Display error
	}

	return m, tea.Batch(cmds...)
}

// handleCommand processes user input from the text field.
func (m *model) handleCommand(input string) tea.Cmd {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return nil // No command entered
	}
	command := strings.ToLower(parts[0])

	switch command {
	case "h", "help":
		m.message = "Commands: s[tep], r[un], p[ause], reset, m[em] <addr>,\n d[isasm], load <file> <addr>, ser[ial], q[uit]"
	case "s", "step":
		// Execute one full instruction
		m.message = "Stepping..."
		// Clock until the current instruction completes
		startCycles := m.cpu.TotalCycles()
		if m.cpu.Cycles == 0 { // If already at boundary, clock once to fetch
			m.cpu.Clock()
		}
		for m.cpu.Cycles > 0 {
			m.cpu.Clock()
		}
		m.message = fmt.Sprintf("Stepped. PC=%04X. Took %d cycles.", m.cpu.PC, m.cpu.TotalCycles()-startCycles)
	case "r", "run":
		if m.mode == RunningMode {
			m.message = "Already running."
			return nil
		}
		m.mode = RunningMode
		m.message = "Running... (Press Ctrl+C or Esc to pause)"
		m.textInput.Blur() // Unfocus input field while running
		return tickCmd()   // Start the execution ticks
	case "p", "pause":
		if m.mode == CommandMode {
			m.message = "Already paused."
			return nil
		}
		m.mode = CommandMode
		m.message = "Execution paused."
		m.textInput.Focus()
		return textinput.Blink
	case "reset":
		m.cpu.Reset()
		m.serial.ClearOutput()
		// Reload program and set PC after reset
		err := m.bus.LoadProgram(defaultLoadAddr, helloWorldProgram)
		if err != nil {
			m.err = err
			m.message = fmt.Sprintf("Error reloading program: %v", err)
		} else {
			m.cpu.PC = defaultLoadAddr
			m.message = "CPU Reset. Program reloaded. PC set to $0200."
		}
	case "m", "mem":
		if len(parts) < 2 {
			m.message = "Usage: m <addr> (e.g., m 0200 or m $C000)"
			return nil
		}
		addrStr := strings.TrimPrefix(parts[1], "$")
		addr, err := strconv.ParseUint(addrStr, 16, 16)
		if err != nil {
			m.err = fmt.Errorf("invalid address format: %w", err)
			m.message = fmt.Sprintf("Error: %v", m.err)
			return nil
		}
		m.memViewAddr = uint16(addr)
		m.message = fmt.Sprintf("Memory view centered at $%04X", m.memViewAddr)
	case "d", "disasm":
		// Disassembly view is updated automatically, but this forces refresh
		m.message = "Disassembly view refreshed."
	case "load", "l": // Load program from file
		if len(parts) != 3 {
			m.err = fmt.Errorf("usage: [l]oad <filepath.bin> <hex_address>")
			m.message = fmt.Sprintf("Error: %v", m.err)
			return nil
		}
		filePath := parts[1]
		addrStr := strings.TrimPrefix(parts[2], "$") // Allow optional $ prefix

		// Parse address
		loadAddr64, err := strconv.ParseUint(addrStr, 16, 16)
		if err != nil {
			m.err = fmt.Errorf("invalid load address format '%s': %w", parts[2], err)
			m.message = fmt.Sprintf("Error: %v", m.err)
			return nil
		}
		loadAddr := uint16(loadAddr64)

		// Read file content
		programData, err := os.ReadFile(filePath)
		if err != nil {
			m.err = fmt.Errorf("failed to read file '%s': %w", filePath, err)
			m.message = fmt.Sprintf("Error: %v", m.err)
			return nil
		}

		// Load into bus memory
		err = m.bus.LoadProgram(loadAddr, programData)
		if err != nil {
			m.err = fmt.Errorf("failed to load program into memory: %w", err)
			m.message = fmt.Sprintf("Error: %v", m.err)
			return nil
		}

		// Success!
		m.message = fmt.Sprintf("Loaded %d bytes from '%s' to $%04X.", len(programData), filepath.Base(filePath), loadAddr)
		// Optional: Set PC automatically? Decided against for now.
		// m.cpu.PC = loadAddr
		// m.message += fmt.Sprintf(" PC set to $%04X.", loadAddr)
		m.updateViews() // Refresh memory/disassembly
	case "ser", "serial":
		// Serial view is updated automatically, this command could be used
		// for other serial actions later (e.g., clear, show status)
		m.message = "Serial output view refreshed."
	case "q", "quit":
		return tea.Quit
	default:
		m.message = fmt.Sprintf("Unknown command: %s. Type 'h' for help.", command)
	}
	return nil
}

// updateViews prepares the display strings for rendering.
func (m *model) updateViews() {
	// --- Disassembly ---
	// Show ~10 lines around PC
	disasmLines := 10
	startAddr := m.cpu.PC
	// Try to center PC, adjusting for start/end of memory
	if startAddr > uint16(disasmLines/2)*3 { // Assume ~3 bytes/instr avg
		startAddr -= uint16(disasmLines/2) * 3
	} else {
		startAddr = 0
	}
	// Read ahead enough bytes for potential instructions
	m.disassembly = m.cpu.Disassemble(startAddr, startAddr+uint16(disasmLines*4)) // Read a bit more

	// --- Memory View ---
	memLines := 8
	bytesPerLine := 16
	memData := m.bus.ReadRange(m.memViewAddr, memLines*bytesPerLine)
	var memBuilder strings.Builder
	for i := 0; i < memLines; i++ {
		lineAddr := m.memViewAddr + uint16(i*bytesPerLine)
		memBuilder.WriteString(fmt.Sprintf("%04X: ", lineAddr))
		lineData := memData[i*bytesPerLine : (i+1)*bytesPerLine]

		// Hex bytes
		for j := 0; j < bytesPerLine; j++ {
			if i*bytesPerLine+j >= len(memData) {
				memBuilder.WriteString("   ") // Padding if data ends
			} else {
				memBuilder.WriteString(fmt.Sprintf("%02X ", lineData[j]))
			}
		}
		memBuilder.WriteString(" ")

		// ASCII representation
		for j := 0; j < bytesPerLine; j++ {
			if i*bytesPerLine+j >= len(memData) {
				memBuilder.WriteByte(' ')
			} else {
				char := lineData[j]
				if char < 32 || char > 126 {
					memBuilder.WriteByte('.')
				} else {
					memBuilder.WriteByte(char)
				}
			}
		}
		memBuilder.WriteString("\n")
	}
	m.memViewContent = strings.TrimRight(memBuilder.String(), "\n")

	// --- Serial Output ---
	m.serialOutput = m.serial.GetOutput()
}

// --- View Rendering ---

func (m model) View() string {
	if m.width == 0 || m.height == 0 { // Check height too
		return "Initializing..."
	}

	// --- Calculate Footer Height ---
	// Measure or estimate the height needed for the input field and help text.
	// Input field with border typically takes 3 lines (1 for text, 2 for top/bottom border).
	// Help text takes 1 line.
	inputHeight := 3 // Based on text + rounded border
	helpHeight := 1
	footerHeight := inputHeight + helpHeight

	// --- Prepare Content Sections ---
	// CPU State (This will be part of the main scrollable/resizable area)
	cpuState := m.styleCPU.Render(m.cpu.GetState())

	// Disassembly (as before)
	var disasmBuilder strings.Builder
	disasmBuilder.WriteString("Disassembly:\n")
	linesRendered := 0
	renderedPCLine := false
	// Get sorted addresses for deterministic rendering
	// NOTE: Disassemble returns a map, iteration order isn't guaranteed.
	// For a truly stable view, sort the keys or disassemble line-by-line.
	// This simplified version might still jump slightly if PC isn't the first key.
	// A more robust way: Disassemble a fixed range *starting* near PC.
	keys := make([]uint16, 0, len(m.disassembly))
	for k := range m.disassembly {
		keys = append(keys, k)
	}
	// Simple sort (might not be perfect instruction order, but better than map)
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}

	for _, addr := range keys {
		line := m.disassembly[addr] // Get line from original map
		lineStr := fmt.Sprintf("%04X: %s", addr, line)
		if addr == m.cpu.PC {
			disasmBuilder.WriteString(m.styleHighlight.Render(lineStr) + "\n")
			renderedPCLine = true
		} else {
			disasmBuilder.WriteString(lineStr + "\n")
		}
		linesRendered++
		if linesRendered >= 15 { // Render a few more lines for context
			break
		}
	}
	// If PC wasn't in the rendered block (e.g., near end of mem), add it
	if !renderedPCLine && linesRendered < 15 {
		op := m.bus.Read(m.cpu.PC)
		instr := m.cpu.LookupTable()[op] // Use the getter method here
		pcLine := fmt.Sprintf("%04X: %s ???", m.cpu.PC, instr.Name)
		disasmBuilder.WriteString(m.styleHighlight.Render(pcLine) + "\n")
	}
	disasmView := m.styleDisasm.Render(strings.TrimSpace(disasmBuilder.String()))

	// Memory View (as before)
	memoryView := m.styleMemory.Render(fmt.Sprintf("Memory @ $%04X:\n%s", m.memViewAddr, m.memViewContent))

	// Serial Output (as before)
	serialRender := m.styleSerial.Render(fmt.Sprintf("Serial Output ($%04X):\n%s", serialAddr, m.serialOutput))

	// Status/Message Area (as before)
	statusMsg := m.message
	if m.err != nil {
		statusMsg = fmt.Sprintf("Error: %v", m.err)
	}
	statusArea := m.styleStatus.Render(statusMsg)

	// --- Layout Panels ---
	// Combine left and right panels like before
	leftPanel := lipgloss.JoinVertical(lipgloss.Left,
		disasmView,
		serialRender,
	)

	rightPanel := lipgloss.JoinVertical(lipgloss.Left,
		memoryView,
		statusArea,
	)

	// Combine CPU state and the main horizontal panels vertically
	// This is the content that needs to fit *above* the footer.
	mainContent := lipgloss.JoinVertical(lipgloss.Left,
		cpuState,
		lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel),
	)

	// --- Calculate Main Content Height ---
	// The main content should fill the space NOT taken by the footer
	mainHeight := m.height - footerHeight
	if mainHeight < 0 { // Prevent negative height if terminal is tiny
		mainHeight = 0
	}

	// --- Render Footer ---
	// Render input and help text separately for the footer
	inputArea := m.styleInput.Render(m.textInput.View())
	helpHint := m.styleHelp.Render("step(s) run(r) pause(p) reset mem(m) disasm(d) load(l) serial(ser) help(h) quit(q)")

	// --- Final Assembly ---
	// Render the main content with a *maximum* height constraint.
	// Lipgloss will handle truncation or vertical overflow if the content is taller.
	mainContentView := lipgloss.NewStyle().
		Height(mainHeight).    // Set the calculated height
		MaxHeight(mainHeight). // Ensure it doesn't grow beyond this
		Width(m.width).        // Use available width
		MaxWidth(m.width).
		Render(mainContent) // Render the combined (CPU + panels) content

	// Use JoinVertical to place the constrained main content *above* the footer elements.
	finalView := lipgloss.JoinVertical(lipgloss.Left,
		mainContentView,
		inputArea,
		helpHint,
	)

	// Return the fully assembled view
	// Note: We don't need the MaxWidth render here anymore as we applied width to mainContentView
	return finalView
}

func main() {
	// --- Logging Setup ---
	logFile, err := os.OpenFile("go6502repl.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("Error opening log file:", err)
		os.Exit(1)
	}
	defer logFile.Close()
	log.SetOutput(logFile)
	log.Println("--- Application Start ---")

	// --- Run Bubbletea ---
	p := tea.NewProgram(initialModel(), tea.WithAltScreen()) // Use AltScreen for cleaner exit
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		log.Printf("Runtime error: %v", err)
		os.Exit(1)
	}
	log.Println("--- Application End ---")
}
