package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMockPaymentFlow(t *testing.T) {
	api := newMockAPI("")
	api.nextPaymentDelay = 60 * time.Millisecond
	server := httptest.NewServer(api.routes())
	defer server.Close()
	api.baseURL = server.URL

	createResponse, err := http.Post(server.URL+"/api/v1/payment", "application/json", strings.NewReader(`{"currency":"CHF"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer createResponse.Body.Close()
	if createResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want %d", createResponse.StatusCode, http.StatusCreated)
	}

	var created payment
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.PaymentPhase != "waiting_for_amount" || created.Status != "waiting" {
		t.Fatalf("unexpected initial payment state: %+v", created)
	}
	if created.AmountSelectionExpiresAt != nil {
		t.Fatal("amount-selection countdown should not start before QR is scanned")
	}
	if _, err := base64.StdEncoding.DecodeString(created.QRCodePNGBase64); err != nil {
		t.Fatalf("mock returned invalid QR PNG: %v", err)
	}

	scannedResponse, err := http.Post(server.URL+"/dev/payment/"+created.ID+"/scanned", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var scanned map[string]bool
	if err := json.NewDecoder(scannedResponse.Body).Decode(&scanned); err != nil {
		scannedResponse.Body.Close()
		t.Fatal(err)
	}
	scannedResponse.Body.Close()
	if scannedResponse.StatusCode != http.StatusOK || !scanned["qr_scanned"] {
		t.Fatalf("QR scan response = %d, %+v", scannedResponse.StatusCode, scanned)
	}

	afterScanResponse, err := http.Get(server.URL + "/api/v1/payment/" + created.ID)
	if err != nil {
		t.Fatal(err)
	}
	var afterScan payment
	if err := json.NewDecoder(afterScanResponse.Body).Decode(&afterScan); err != nil {
		afterScanResponse.Body.Close()
		t.Fatal(err)
	}
	afterScanResponse.Body.Close()
	if afterScan.PaymentPhase != "waiting_for_amount" || afterScan.AmountSelectionExpiresAt == nil {
		t.Fatalf("QR scan should start the countdown without advancing payment phase: %+v", afterScan)
	}

	amountResponse, err := http.Post(server.URL+"/dev/payment/"+created.ID+"/amount", "application/json", strings.NewReader(`{"amount_cents":2000}`))
	if err != nil {
		t.Fatal(err)
	}
	amountResponse.Body.Close()
	if amountResponse.StatusCode != http.StatusOK {
		t.Fatalf("amount status = %d, want %d", amountResponse.StatusCode, http.StatusOK)
	}

	statusResponse, err := http.Get(server.URL + "/api/v1/payment/" + created.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer statusResponse.Body.Close()
	var selected payment
	if err := json.NewDecoder(statusResponse.Body).Decode(&selected); err != nil {
		t.Fatal(err)
	}
	if selected.PaymentPhase != "waiting_for_payment" || selected.AmountCents == nil || *selected.AmountCents != 2000 {
		t.Fatalf("unexpected payment after amount selection: %+v", selected)
	}

	paidAt := time.Now()
	paidResponse, err := http.Post(server.URL+"/dev/payment/"+created.ID+"/paid", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	paidResponse.Body.Close()
	if paidResponse.StatusCode != http.StatusOK {
		t.Fatalf("paid status = %d, want %d", paidResponse.StatusCode, http.StatusOK)
	}
	finalResponse, err := http.Get(server.URL + "/api/v1/payment/" + created.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer finalResponse.Body.Close()
	var final payment
	if err := json.NewDecoder(finalResponse.Body).Decode(&final); err != nil {
		t.Fatal(err)
	}
	if final.Status != "paid" {
		t.Fatalf("final status = %q, want paid", final.Status)
	}

	nextResponse, err := http.Post(server.URL+"/api/v1/payment", "application/json", strings.NewReader(`{"currency":"CHF"}`))
	if err != nil {
		t.Fatal(err)
	}
	nextResponse.Body.Close()
	if nextResponse.StatusCode != http.StatusCreated {
		t.Fatalf("next payment status = %d, want %d", nextResponse.StatusCode, http.StatusCreated)
	}
	if elapsed := time.Since(paidAt); elapsed < api.nextPaymentDelay {
		t.Fatalf("next payment created after %v, want at least %v", elapsed, api.nextPaymentDelay)
	}
}

func TestDeviceSupportEndpoints(t *testing.T) {
	api := newMockAPI("")
	server := httptest.NewServer(api.routes())
	defer server.Close()

	statusResponse, err := http.Post(server.URL+"/api/v1/device/status", "application/json", strings.NewReader(`{"dispensed_count":0}`))
	if err != nil {
		t.Fatal(err)
	}
	statusResponse.Body.Close()
	if statusResponse.StatusCode != http.StatusOK {
		t.Fatalf("device status response = %d, want %d", statusResponse.StatusCode, http.StatusOK)
	}

	commandResponse, err := http.Get(server.URL + "/api/v1/device/commands")
	if err != nil {
		t.Fatal(err)
	}
	commandResponse.Body.Close()
	if commandResponse.StatusCode != http.StatusOK {
		t.Fatalf("command response = %d, want %d", commandResponse.StatusCode, http.StatusOK)
	}

	logResponse, err := http.Post(server.URL+"/api/v1/device/logs", "application/x-ndjson", strings.NewReader("one\ntwo\n"))
	if err != nil {
		t.Fatal(err)
	}
	logResponse.Body.Close()
	if logResponse.StatusCode != http.StatusCreated {
		t.Fatalf("log response = %d, want %d", logResponse.StatusCode, http.StatusCreated)
	}
}
