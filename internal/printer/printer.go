// Package printer drives an ESC/POS receipt printer via raw device writes,
// mirroring the ticket layout of the original Python cafi-karaoke script.
package printer

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jsalamander/baendaeli-client/internal/config"
)

const (
	contentFeedLinesBefore = 4
	feedLinesAfter         = 12
	paymentFeedLinesBefore = 1
	paymentFeedLinesAfter  = 15

	defaultDevicePath = "/dev/usb/lp0"

	itemName = "Solibändeli"
)

var zurich *time.Location

func init() {
	loc, err := time.LoadLocation("Europe/Zurich")
	if err != nil {
		loc = time.Local
	}
	zurich = loc
}

// Printer writes tickets to a raw ESC/POS printer device.
type Printer struct {
	enabled    bool
	devicePath string
	sim        bool
}

func New(cfg *config.Config) *Printer {
	if cfg == nil {
		return &Printer{}
	}
	return &Printer{
		enabled:    cfg.IsPrinterEnabled(),
		devicePath: cfg.PrinterDevicePath,
	}
}

func (p *Printer) IsEnabled() bool {
	return p != nil && p.enabled
}

func (p *Printer) IsSimulation() bool {
	return p != nil && p.sim
}

// Init re-syncs config and falls back to simulation mode if the device is unusable.
func (p *Printer) Init(cfg *config.Config) error {
	if p == nil {
		return nil
	}
	if cfg != nil {
		p.enabled = cfg.IsPrinterEnabled()
		if cfg.PrinterDevicePath != "" {
			p.devicePath = cfg.PrinterDevicePath
		}
	}
	if !p.enabled {
		log.Println("Printer: disabled via PRINTER_ENABLED, receipts will not be printed")
		return nil
	}
	if p.devicePath == "" {
		p.devicePath = defaultDevicePath
	}

	// Open here so permission problems surface at startup rather than at print time.
	f, err := os.OpenFile(p.devicePath, os.O_WRONLY, 0)
	if err != nil {
		log.Printf("Printer: device %s unusable, running in simulation mode: %v", p.devicePath, err)
		p.sim = true
		return nil
	}
	f.Close()

	log.Printf("Printer: initialised on %s", p.devicePath)
	return nil
}

func (p *Printer) Close() error {
	return nil
}

// PrintPaymentTicket prints a ticket for a completed payment.
func (p *Printer) PrintPaymentTicket(paymentID string, amountCents int64, dispensedCount int) error {
	if skipped := p.skipReason(); skipped != "" {
		log.Printf("Printer: skipping payment ticket (%s)", skipped)
		return nil
	}

	if ready, err := p.checkPrinterReady(); err != nil || !ready {
		if err != nil {
			return fmt.Errorf("printer status check failed: %w", err)
		}
		return fmt.Errorf("printer not ready")
	}

	body := []byte{}
	body = append(body, 0x1d, '!', 0x11) // double width/height
	body = append(body, []byte("NBNN 26\n\n")...)
	body = append(body, 0x1d, '!', 0x00) // normal font
	body = append(body, []byte(quantityLine(dispensedCount)+"\n")...)
	body = append(body, []byte(formatAmount(amountCents)+"\n\n")...)
	body = append(body, []byte(formatTimestamp()+"\n\n")...)
	body = append(body, []byte(paymentID+"\n")...)

	if err := p.writePaymentTicket(body); err != nil {
		return err
	}

	log.Printf("Printer: payment ticket printed payment_id=%s amount_cents=%d dispensed_count=%d", paymentID, amountCents, dispensedCount)
	return nil
}

// PrintStartupMessage prints the printer-ready ticket once when the client starts.
func (p *Printer) PrintStartupMessage() error {
	if skipped := p.skipReason(); skipped != "" {
		log.Printf("Printer: skipping startup ticket (%s)", skipped)
		return nil
	}

	if ready, err := p.checkPrinterReady(); err != nil || !ready {
		if err != nil {
			return fmt.Errorf("printer status check failed: %w", err)
		}
		return fmt.Errorf("printer not ready")
	}

	body := []byte{}
	body = append(body, 0x1d, '!', 0x11)
	body = append(body, []byte("NBNN\n\n")...)
	body = append(body, 0x1d, '!', 0x00)
	body = append(body, []byte(formatTimestamp()+"\n")...)
	body = append(body, []byte("Solibändeli Ready!!\n")...)

	if err := p.writeTicket(body); err != nil {
		return err
	}

	log.Println("Printer: startup ticket printed")
	return nil
}

// PrintText prints arbitrary text, used by the `print` CLI command for diagnostics.
func (p *Printer) PrintText(text string) error {
	if skipped := p.skipReason(); skipped != "" {
		return fmt.Errorf("printing skipped: %s", skipped)
	}

	if ready, err := p.checkPrinterReady(); err != nil || !ready {
		if err != nil {
			return fmt.Errorf("printer status check failed: %w", err)
		}
		return fmt.Errorf("printer not ready")
	}

	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}

	return p.writeTicket([]byte(text))
}

func (p *Printer) writePaymentTicket(body []byte) error {
	return p.writeTicketWithMargins(body, paymentFeedLinesBefore, paymentFeedLinesAfter, true)
}

// skipReason returns a non-empty explanation when printing should be skipped.
func (p *Printer) skipReason() string {
	switch {
	case p == nil:
		return "printer not configured"
	case !p.enabled:
		return "disabled via PRINTER_ENABLED"
	case p.sim:
		return "simulation mode, device " + p.devicePath + " unusable"
	default:
		return ""
	}
}

// quantityLine renders the fixed business-rule quantity line for the payment ticket.
func quantityLine(dispensedCount int) string {
	if dispensedCount <= 0 {
		return fmt.Sprintf("0x %s ERROR", itemName)
	}
	return fmt.Sprintf("1x %s", itemName)
}

func formatAmount(amountCents int64) string {
	return fmt.Sprintf("%.2f CHF", float64(amountCents)/100.0)
}

func formatTimestamp() string {
	return time.Now().In(zurich).Format("2006-01-02 15:04:05")
}

// checkPrinterReady sends ESC/POS DLE EOT 4 and inspects the returned paper status byte.
func (p *Printer) checkPrinterReady() (bool, error) {
	f, err := os.OpenFile(p.devicePath, os.O_RDWR, 0)
	if err != nil {
		return false, fmt.Errorf("failed to open printer: %w", err)
	}
	defer f.Close()

	if _, err := f.Write([]byte{0x10, 0x04, 0x04}); err != nil {
		return false, fmt.Errorf("failed to write status query: %w", err)
	}

	status := make([]byte, 1)
	n, err := f.Read(status)
	if err != nil || n == 0 {
		return false, fmt.Errorf("printer did not return a status byte: %w", err)
	}

	s := status[0]
	log.Printf("Printer: status 0x%02X", s)

	if s&0x20 != 0 {
		log.Println("Printer: reports NO PAPER")
		return false, nil
	}
	if s&0x40 != 0 {
		log.Println("Printer: reports PAPER END")
		return false, nil
	}

	return true, nil
}

// writeTicket initialises the printer, feeds tear margins around the body, and flushes.
func (p *Printer) writeTicket(body []byte) error {
	return p.writeTicketWithMargins(body, contentFeedLinesBefore, feedLinesAfter, false)
}

func (p *Printer) writeTicketWithMargins(body []byte, leadingLines, trailingLines int, withFooter bool) error {
	f, err := os.OpenFile(p.devicePath, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("failed to open printer: %w", err)
	}
	defer f.Close()

	if _, err := f.Write([]byte{0x1b, '@'}); err != nil { // initialise printer
		return fmt.Errorf("failed to initialise printer: %w", err)
	}
	if _, err := f.Write([]byte{0x1b, 't', 0x00}); err != nil { // select CP437
		return fmt.Errorf("failed to select printer code page: %w", err)
	}
	if _, err := f.Write([]byte{0x1b, 'a', 0x01}); err != nil { // centre align
		return fmt.Errorf("failed to centre ticket: %w", err)
	}
	if _, err := f.Write([]byte{0x1b, 'E', 0x01}); err != nil { // bold on
		return fmt.Errorf("failed to enable bold font: %w", err)
	}
	if _, err := f.Write([]byte(strings.Repeat("\n", leadingLines))); err != nil {
		return fmt.Errorf("failed to feed before ticket content: %w", err)
	}
	if _, err := f.Write(printerText(string(body))); err != nil {
		return fmt.Errorf("failed to write ticket: %w", err)
	}
	if _, err := f.Write([]byte{0x1b, 'E', 0x00, 0x1d, '!', 0x00}); err != nil { // bold off, reset font size
		return fmt.Errorf("failed to reset printer formatting: %w", err)
	}
	if _, err := f.Write([]byte(strings.Repeat("\n", trailingLines))); err != nil {
		return fmt.Errorf("failed to feed after ticket: %w", err)
	}
	if withFooter {
		if _, err := f.Write([]byte("<3\n")); err != nil {
			return fmt.Errorf("failed to write ticket footer: %w", err)
		}
	}

	return nil
}

func printerText(text string) []byte {
	return []byte(strings.ReplaceAll(text, "ä", string([]byte{0x84})))
}
