package device

import (
	"log"
	"time"
)

const (
	leverPollInterval = 10 * time.Millisecond
	leverDebounce     = 50 * time.Millisecond
)

type leverRequest struct {
	paymentID  string
	generation uint64
}

type leverDebouncer struct {
	rawActive bool
	changedAt time.Time
	armed     bool
}

func (d *leverDebouncer) update(active bool, now time.Time) bool {
	if d.changedAt.IsZero() || active != d.rawActive {
		d.rawActive = active
		d.changedAt = now
	}
	if now.Sub(d.changedAt) < leverDebounce {
		return false
	}
	if !active {
		d.armed = true
		return false
	}
	if !d.armed {
		return false
	}
	d.armed = false
	return true
}

func (c *Client) monitorLever() {
	defer c.wg.Done()
	ticker := time.NewTicker(leverPollInterval)
	defer ticker.Stop()
	var debouncer leverDebouncer
	var press leverRequest
	var pressEligible bool

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			active, err := c.leverSwitch.ReadActive()
			if err != nil {
				log.Printf("Device client: lever read failed; free dispense unavailable: %v", err)
				return
			}
			if active && !debouncer.rawActive {
				press, pressEligible = c.currentLeverRequest()
			}
			if !debouncer.update(active, time.Now()) {
				continue
			}
			request, eligible := c.currentLeverRequest()
			if !pressEligible || !eligible || request != press {
				continue
			}
			select {
			case c.leverRequests <- request:
			default:
				log.Println("Device client: lever request ignored; a request is already pending")
			}
		}
	}
}

func (c *Client) currentLeverRequest() (leverRequest, bool) {
	if c.config == nil || !c.config.LeverEnabled || c.jammed.Load() {
		return leverRequest{}, false
	}
	c.statusMutex.Lock()
	defer c.statusMutex.Unlock()
	c.paymentIDMutex.Lock()
	defer c.paymentIDMutex.Unlock()

	if c.state != StateAwaitingPayment || c.currentPaymentID == "" ||
		c.pendingDispensedCount(c.currentPaymentID) != nil {
		return leverRequest{}, false
	}
	return leverRequest{paymentID: c.currentPaymentID, generation: c.stateGeneration}, true
}

// Requests are handled on the device poll loop, never on the GPIO monitor.
func (c *Client) handleLeverRequest(request leverRequest) {
	c.actuatorMutex.Lock()
	defer c.actuatorMutex.Unlock()
	current, eligible := c.currentLeverRequest()
	if !eligible || current != request || c.ctx.Err() != nil {
		return
	}

	log.Printf("Device client: lever free dispense for pending payment %s", request.paymentID)
	c.setRuntimeState(StateDispensing, "Gratis-Ausgabe laeuft")
	c.setExecutingCommand(&CommandResponse{
		Command: "message",
		Message: "Gratis-Ausgabe läuft",
	})

	// A free ball must not mark the retained payment as already dispensed.
	if _, err := c.dispenseAndWaitForBallForPaymentLocked(""); err != nil {
		if !c.jammed.Load() {
			c.setRuntimeState(StateError, "Gratis-Ausgabe fehlgeschlagen")
			c.clearExecutingCommand()
		}
		log.Printf("Device client: lever free dispense failed: %v", err)
		return
	}

	c.setRuntimeState(StateAwaitingPayment, "Warten auf Zahlung")
	c.setExecutingCommand(&CommandResponse{
		Command: "message",
		Message: "Warten auf Zahlung",
	})
}
