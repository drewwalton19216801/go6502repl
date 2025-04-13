package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	cpu "github.com/drewwalton19216801/sixty502" // Use the original path from the prompt
	// Or, if using local module path:
	// cpu "<your-module-path>/cpu6502"
)

type model struct {
	cpu       *cpu.CPU
	ram       *RAM
	viewport  viewport.Model
	textInput textinput.Model
	err       error
	ready     bool     // Flag to indicate if viewport is initialized
	history   []string // Simple command history
	histPos   int
}

const (
	historyMax = 50
)

func initialModel() model {
	ram := NewRAM()
	cpuInstance := cpu.NewCPU(ram)
	cpuInstance.Reset() // Start in a known state

	ti := textinput.New()
	ti.Placeholder = "Enter command (help, load, step, mem, set, reset, quit)..."
	ti.Focus()
	ti.CharLimit = 156
	ti.Width = 20 // Initial width, will be updated

	// Viewport will be initialized fully in Update when size is known
	vp := viewport.New(0, 0) // Initial dummy size

	return model{
		cpu:       cpuInstance,
		ram:       ram,
		textInput: ti,
		viewport:  vp,
		histPos:   -1, // No history selected
	}
}

func (m model) Init() tea.Cmd {
	return textinput.Blink // Start the cursor blinking
}

// Helper to update viewport content and scroll to bottom
func (m *model) updateViewport(content string) {
	if !m.ready {
		return // Don't update if not ready
	}
	m.viewport.SetContent(content)
	m.viewport.GotoBottom()
}

// Generates the status/help/CPU state string for the viewport
func (m *model) generateStatus() string {
	cpuState := m.cpu.GetState()
	errorMessage := ""
	if m.err != nil {
		errorMessage = fmt.Sprintf("\nError: %v", m.err)
		m.err = nil // Clear error after displaying
	}
	helpHint := "\nCommands: help, load <f> <addr>, step, mem <addr> [n], set <reg|mem> ..., reset, quit"
	return cpuState + errorMessage + helpHint
}

// Parses hex numbers (e.g., $FF, 0xFF, FF)
func parseHex16(s string) (uint16, error) {
	s = strings.TrimPrefix(s, "$")
	s = strings.TrimPrefix(s, "0x")
	val, err := strconv.ParseUint(s, 16, 16)
	if err != nil {
		return 0, fmt.Errorf("invalid hex address '%s': %w", s, err)
	}
	return uint16(val), nil
}

func parseHex8(s string) (uint8, error) {
	s = strings.TrimPrefix(s, "$")
	s = strings.TrimPrefix(s, "0x")
	val, err := strconv.ParseUint(s, 16, 8)
	if err != nil {
		return 0, fmt.Errorf("invalid hex value '%s': %w", s, err)
	}
	return uint8(val), nil
}

// --- Command Handlers ---

func (m *model) handleLoad(args []string) {
	if len(args) != 3 {
		m.err = fmt.Errorf("usage: load <filename> <hex_address>")
		return
	}
	filename := args[1]
	addrStr := args[2]

	addr, err := parseHex16(addrStr)
	if err != nil {
		m.err = err
		return
	}

	data, err := ioutil.ReadFile(filename)
	if err != nil {
		m.err = fmt.Errorf("could not read file '%s': %w", filename, err)
		return
	}

	err = m.ram.Load(addr, data)
	if err != nil {
		m.err = err
		return
	}
	m.updateViewport(fmt.Sprintf("Loaded %d bytes from '%s' to $%04X.\n%s", len(data), filename, addr, m.generateStatus()))
}

func (m *model) handleStep() {
	// Execute cycles until the current instruction is complete
	startCycles := m.cpu.TotalCycles()
	if m.cpu.Cycles == 0 {
		m.cpu.Clock() // Fetch and start the next instruction if needed
	}
	for m.cpu.Cycles > 0 {
		m.cpu.Clock()
	}
	// Ensure at least one full instruction cycle has passed if we started mid-instruction
	if m.cpu.Cycles == 0 && m.cpu.TotalCycles() == startCycles {
		m.cpu.Clock() // Fetch next
		for m.cpu.Cycles > 0 {
			m.cpu.Clock()
		}
	}

	// Optional: Show disassembly of *next* instruction
	disasm := m.cpu.Disassemble(m.cpu.PC, m.cpu.PC+3) // Look ahead a bit
	nextInstrStr := ""
	if line, ok := disasm[m.cpu.PC]; ok {
		nextInstrStr = "\nNext: " + line
	}

	m.updateViewport(m.generateStatus() + nextInstrStr)
}

func (m *model) handleMem(args []string) {
	if len(args) < 2 {
		m.err = fmt.Errorf("usage: mem <hex_address> [count]")
		return
	}
	addrStr := args[1]
	count := 16 // Default count

	addr, err := parseHex16(addrStr)
	if err != nil {
		m.err = err
		return
	}

	if len(args) > 2 {
		countVal, err := strconv.Atoi(args[2])
		if err != nil || countVal <= 0 {
			m.err = fmt.Errorf("invalid count '%s'", args[2])
			return
		}
		count = countVal
	}

	memDump := m.ram.Dump(addr, count)
	m.updateViewport(memDump + m.generateStatus())
}

func (m *model) handleSet(args []string) {
	if len(args) < 4 {
		m.err = fmt.Errorf("usage: set <reg|mem> <name|address> <value>")
		return
	}
	targetType := strings.ToLower(args[1])
	targetName := args[2]
	valueStr := args[3]

	switch targetType {
	case "reg":
		val, err := parseHex8(valueStr) // Most registers are 8-bit
		if err != nil && strings.ToUpper(targetName) != "PC" && strings.ToUpper(targetName) != "P" {
			m.err = err
			return
		}

		switch strings.ToUpper(targetName) {
		case "A":
			m.cpu.A = val
		case "X":
			m.cpu.X = val
		case "Y":
			m.cpu.Y = val
		case "SP":
			m.cpu.SP = val
		case "PC":
			pcVal, err := parseHex16(valueStr) // PC is 16-bit
			if err != nil {
				m.err = err
				return
			}
			m.cpu.PC = pcVal
		case "P":
			// Allow setting flags via hex value
			pVal, err := parseHex8(valueStr)
			if err != nil {
				m.err = err
				return
			}
			m.cpu.P = cpu.Flags(pVal) | cpu.U // Ensure U is always set
		default:
			m.err = fmt.Errorf("unknown register: %s (Valid: A, X, Y, SP, PC, P)", targetName)
			return
		}
		m.updateViewport(m.generateStatus())

	case "mem":
		addr, err := parseHex16(targetName)
		if err != nil {
			m.err = err
			return
		}
		val, err := parseHex8(valueStr)
		if err != nil {
			m.err = err
			return
		}
		m.ram.Write(addr, val)
		m.updateViewport(fmt.Sprintf("Wrote $%02X to $%04X.\n%s", val, addr, m.generateStatus()))

	default:
		m.err = fmt.Errorf("invalid set target type: %s (Valid: reg, mem)", targetType)
	}
}

func (m *model) handleGet(args []string) {
	if len(args) < 3 {
		m.err = fmt.Errorf("usage: get <reg|mem> <name|address>")
		return
	}
	targetType := strings.ToLower(args[1])
	targetName := args[2]

	var output string

	switch targetType {
	case "reg":
		regNameUpper := strings.ToUpper(targetName)
		switch regNameUpper {
		case "A":
			output = fmt.Sprintf("Register A = $%02X (%d)", m.cpu.A, m.cpu.A)
		case "X":
			output = fmt.Sprintf("Register X = $%02X (%d)", m.cpu.X, m.cpu.X)
		case "Y":
			output = fmt.Sprintf("Register Y = $%02X (%d)", m.cpu.Y, m.cpu.Y)
		case "SP":
			output = fmt.Sprintf("Register SP = $%02X", m.cpu.SP)
		case "PC":
			output = fmt.Sprintf("Register PC = $%04X", m.cpu.PC)
		case "P":
			// Use the existing formatter
			flagsStr := cpu.FormatFlags(m.cpu.P)
			output = fmt.Sprintf("Register P = $%02X [%s]", uint8(m.cpu.P), flagsStr)
		default:
			m.err = fmt.Errorf("unknown register: %s (Valid: A, X, Y, SP, PC, P)", targetName)
			m.updateViewport(m.generateStatus()) // Show error and status
			return
		}
		m.updateViewport(output + "\n" + m.generateStatus())

	case "mem":
		addr, err := parseHex16(targetName)
		if err != nil {
			m.err = err
			m.updateViewport(m.generateStatus()) // Show error and status
			return
		}
		val := m.ram.Read(addr)
		output = fmt.Sprintf("Memory [$%04X] = $%02X (%d)", addr, val, val)
		m.updateViewport(output + "\n" + m.generateStatus())

	default:
		m.err = fmt.Errorf("invalid get target type: %s (Valid: reg, mem)", targetType)
		m.updateViewport(m.generateStatus()) // Show error and status
	}
}

func (m *model) handleHelp() {
	helpText := `
Available Commands:
  help                   Show this help message.
  load <file> <addr>     Load binary <file> into memory at <addr> (e.g., load prog.bin $C000).
  step / s               Execute the next CPU instruction.
  reset                  Reset the CPU state.
  mem <addr> [count]     Show memory starting at <addr> (hex) for [count] bytes (default 16).
                         (e.g., mem $0100, mem $C000 32)
  set reg <reg> <val>    Set CPU register <reg> (A, X, Y, SP, PC, P) to <val> (hex).
                         (e.g., set reg A $FF, set reg PC $C000)
  set mem <addr> <val>   Write byte <val> (hex) to memory address <addr> (hex).
                         (e.g., set mem $0200 $A9)
  get reg <reg>          Show the value of CPU register <reg> (A, X, Y, SP, PC, P).
                         (e.g., get reg P, get reg PC)
  get mem <addr>         Show the byte value at memory address <addr> (hex).
                         (e.g., get mem $0200)
  quit / exit            Exit the REPL.
  Up/Down Arrows         Navigate command history.
`
	m.updateViewport(helpText + "\n" + m.generateStatus())
}

// Adds command to history
func (m *model) addHistory(cmd string) {
	if cmd == "" {
		return
	}
	// Avoid adding consecutive duplicates
	if len(m.history) > 0 && m.history[len(m.history)-1] == cmd {
		return
	}
	m.history = append(m.history, cmd)
	if len(m.history) > historyMax {
		m.history = m.history[len(m.history)-historyMax:]
	}
	m.histPos = len(m.history) // Reset history position after adding
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd // Use slice for multiple commands

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyUp:
			if len(m.history) > 0 {
				if m.histPos == len(m.history) { // If at the new command line
					m.histPos = len(m.history) - 1
					m.textInput.SetValue(m.history[m.histPos])
				} else if m.histPos > 0 {
					m.histPos--
					m.textInput.SetValue(m.history[m.histPos])
				}
				m.textInput.CursorEnd()
			}
		case tea.KeyDown:
			if len(m.history) > 0 && m.histPos < len(m.history) {
				if m.histPos == len(m.history)-1 {
					m.histPos = len(m.history) // Go to "new command" line
					m.textInput.SetValue("")
				} else {
					m.histPos++
					m.textInput.SetValue(m.history[m.histPos])
					m.textInput.CursorEnd()
				}
			}

		case tea.KeyEnter:
			input := strings.TrimSpace(m.textInput.Value())
			m.addHistory(input) // Add command to history
			m.textInput.Reset()
			m.histPos = len(m.history) // Reset history position

			if input == "" {
				break
			}

			parts := strings.Fields(input)
			command := strings.ToLower(parts[0])

			switch command {
			case "quit", "exit":
				return m, tea.Quit
			case "load":
				m.handleLoad(parts)
			case "step", "s":
				m.handleStep()
			case "mem":
				m.handleMem(parts)
			case "set":
				m.handleSet(parts)
			case "get":
				m.handleGet(parts)
			case "reset":
				m.cpu.Reset()
				m.updateViewport("CPU Reset.\n" + m.generateStatus())
			case "help":
				m.handleHelp()
			default:
				m.err = fmt.Errorf("unknown command: %s", command)
				m.updateViewport(m.generateStatus()) // Show error
			}

		default:
			// Allow viewport scrolling when not focused on text input (future enhancement maybe)
			// For now, only handle input keys
			m.textInput, cmd = m.textInput.Update(msg)
			cmds = append(cmds, cmd)
		}

	case tea.WindowSizeMsg:
		// Set up viewport and text input width on initial size message
		headerHeight := 1 // For CPU state line maybe (adjust as needed)
		footerHeight := 1 // For input line

		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-headerHeight-footerHeight)
			m.viewport.YPosition = headerHeight
			m.ready = true
			m.updateViewport(m.generateStatus()) // Initial content
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - headerHeight - footerHeight
		}
		m.textInput.Width = msg.Width - 2 // Leave some padding

	default:
		// Handle other messages (like Blink)
		m.textInput, cmd = m.textInput.Update(msg)
		cmds = append(cmds, cmd)

		// Update viewport only if necessary (less frequent updates might be better)
		// m.viewport, cmd = m.viewport.Update(msg)
		// cmds = append(cmds, cmd)
	}

	// Handle viewport updates separately if needed, e.g., for scrolling keys
	// but usually SetContent is enough after commands.
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...) // Combine commands
}

var (
	// Basic styling
	inputStyle = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(0, 1)
)

func (m model) View() string {
	if !m.ready {
		return "Initializing..."
	}
	// Combine viewport content and text input
	return fmt.Sprintf(
		"%s\n%s",
		m.viewport.View(),                     // The main content area
		inputStyle.Render(m.textInput.View()), // The input field at the bottom
	)
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen()) // Use AltScreen for cleaner exit

	if err := p.Start(); err != nil {
		log.Fatalf("Error running program: %v", err)
		os.Exit(1)
	}
}
