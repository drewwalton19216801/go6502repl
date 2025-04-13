; helloworld.asm - Prints "Hello World!" to the serial port ($F001)
; Assemble with: ca65 helloworld.asm -o helloworld.o
; Link with:     ld65 -o helloworld.bin -C memory.cfg helloworld.o
; (Requires a simple memory.cfg file, see below)

.setcpu "6502"      ; Target CPU
.feature labels_without_colons ; Optional: Allows labels without trailing ':'

; --- Constants ---
SERIAL_PORT = $F001 ; Memory address for serial output

; --- Segments ---
; Define where code and data will reside. These names are conventional.
.segment "CODE"     ; Program code segment
.segment "DATA"     ; Initialized data segment
.segment "ZEROPAGE" ; Zero page variables (none used here)

; --- Code Segment ---
.segment "CODE"
.proc main          ; Define the main procedure (helps with label scope)
    .export start   ; Make the 'start' label globally visible

    ; Let the linker place code based on memory.cfg

start:
    ldx #$00        ; Initialize string index X to 0

loop:
    ; Load character from string using label 'hello_string' and index X.
    ; The linker will resolve the address of 'hello_string'.
    lda hello_string, x

    ; Check if the loaded byte is the null terminator ($00).
    ; LDA sets the Zero flag if A becomes 0.
    beq end_loop    ; Branch if Zero flag is set (end of string)

    ; Write the character in A to the serial port address.
    sta SERIAL_PORT

    inx             ; Increment string index
    jmp loop        ; Repeat for the next character

end_loop:
    jmp end_loop             ; Infinite loop

.endproc

; --- Data Segment ---
.segment "DATA"
hello_string:
    .byte "Hello World!" ; Use .byte to define the ASCII string
    .byte $0A           ; Add the Line Feed character manually
    .byte $00           ; Add the null terminator manually

; --- Vector Table Segment ---
; The linker will place this segment starting at $FFFA as per memory.cfg
.segment "VECTORS"
  .addr nmi_handler   ; $FFFA/$FFFB - Non-Maskable Interrupt vector
  .addr main::start         ; $FFFC/$FFFD - Reset vector (points to our code entry)
  .addr irq_handler   ; $FFFE/$FFFF - Interrupt Request vector

; --- Dummy Interrupt Handlers (place in CODE segment) ---
; These are defined outside 'main' proc, so they are global by default
.segment "CODE"
nmi_handler:
irq_handler:
    rti             ; Return from Interrupt (essential minimal handler)

.segment "VECTORS"  ; Vector table segment