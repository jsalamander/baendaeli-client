// DOM Elements
const statusEl = document.getElementById('status');
const qrEl = document.getElementById('qr');
const errorEl = document.getElementById('error');
const errorContainer = document.getElementById('errorContainer');
const retryBtn = document.getElementById('retry');
const paymentIdEl = document.getElementById('paymentId');
const successBanner = document.getElementById('successBanner');
const internetDot = document.getElementById('internetDot');
const internetStatusText = document.getElementById('internetStatusText');
const gatewayDot = document.getElementById('gatewayDot');
const gatewayStatusText = document.getElementById('gatewayStatusText');
const gatewayMeta = document.getElementById('gatewayMeta');
const expiryMeta = document.getElementById('expiryMeta');

// Configuration (provided by Go template)
const defaultAmount = {{ .DefaultAmount }};
const successOverlayMs = {{ .SuccessOverlayMs }};

// State
let pollTimer = null;
let currentPaymentId = null;
let expiryAt = null;
let expiryTimer = null;

// Event Listeners
retryBtn.addEventListener('click', () => {
	retryBtn.classList.add('hidden');
	errorEl.textContent = '';
	errorContainer.classList.add('hidden');
	qrEl.innerHTML = getLoadingSpinner();
	paymentIdEl.classList.add('hidden');
	clearPoll();
	clearExpiry();
	start();
});

// Initialization
document.addEventListener('DOMContentLoaded', () => {
	setDiagnosticsPending();
	startInternetCheck();
	start();
});

function getLoadingSpinner() {
	return '<div class="text-center"><svg class="inline-block w-8 h-8 animate-spin" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg><p class="mt-2 text-sm">Warten auf QR Code...</p></div>';
}

function clearPoll() {
	if (pollTimer) {
		clearTimeout(pollTimer);
		pollTimer = null;
	}
}

function clearExpiry() {
	if (expiryTimer) {
		clearInterval(expiryTimer);
		expiryTimer = null;
	}
	expiryAt = null;
	expiryMeta.textContent = 'Gültig für --:--';
}

async function start() {
	updateStatus('Zahlungsformular wird erstellt...', 'badge-primary');
	paymentIdEl.classList.add('hidden');
	errorEl.textContent = '';
	errorContainer.classList.add('hidden');
	successBanner.classList.add('hidden');
	qrEl.classList.remove('hidden');
	clearPoll();
	clearExpiry();
	setDiagnosticsPending();
	
	try {
		const data = await createPayment(defaultAmount);
		
		updateStatus('Zahlung wird erstellt...', 'badge-primary');
		if (data.id) {
			paymentIdEl.textContent = 'ID ' + data.id;
			paymentIdEl.classList.remove('hidden');
			currentPaymentId = data.id;
			pollStatus(data.id);
		}
		startExpiryCountdown(data.expires_at, data.valid_for_minutes);
		renderQr(data);
	} catch (err) {
		console.error('Payment creation failed', err);
		updateStatus('Etwas ist schiefgelaufen.', 'badge-error');
		showError('Die Zahlung konnte nicht gestartet werden. Bitte versuche es erneut.');
		retryBtn.classList.remove('hidden');
	}
}

async function pollStatus(id) {
	if (!id) return;
	clearPoll();
	const attemptStarted = performance.now();
	let diagUpdated = false;
	try {
		const timestamp = Date.now();
		const data = await getPaymentStatus(id, null, timestamp);
		const latencyMs = Math.round(performance.now() - attemptStarted);

		if (!data) {
			return;
		}

		updateDiagnostics({ ok: true, latencyMs, at: timestamp });
		diagUpdated = true;
		errorEl.textContent = '';
		errorContainer.classList.add('hidden');

		const status = (data.status || '').toLowerCase();
		switch (status) {
			case 'waiting':
			updateStatus('Warten auf Zahlung...', 'badge-warning');
				pollTimer = setTimeout(() => pollStatus(id), 2000);
				break;
			case 'success':
				updateStatus('Zahlung erfolgreich', 'badge-success');
				showSuccessThenRestart();
				break;
			case 'failure':
				updateStatus('Zahlung fehlgeschlagen', 'badge-error');
				setTimeout(() => start(), 1200);
				break;
			default:
				updateStatus('Unbekannter Status', 'badge-warning');
				pollTimer = setTimeout(() => pollStatus(id), 3000);
		}
	} catch (err) {
		console.error('Status check failed', err);
		if (!diagUpdated) {
			updateDiagnostics({ ok: false, latencyMs: null, at: Date.now() });
		}
		showError('Der Zahlungsstatus konnte nicht geprüft werden. Wir versuchen es gleich erneut.');
		updateStatus('Status wird überprüft...', 'badge-warning');
		pollTimer = setTimeout(() => pollStatus(id), 3000);
	}
}

async function showSuccessThenRestart() {
	successBanner.classList.remove('hidden');
	qrEl.classList.add('hidden');
	clearPoll();
	clearExpiry();
	
	let actuatorTimeMs = successOverlayMs;
	
	try {
		const res = await fetch('/api/actuate', { method: 'POST' });
		const data = await safeParseJson(res);
		
		if (res.ok && data.total_time_ms) {
			actuatorTimeMs = Math.max(successOverlayMs, data.total_time_ms);
			console.log('Actuator completed in', data.total_time_ms, 'ms');
		} else if (!res.ok) {
			console.error('Actuator error:', data.error);
			showError(data.error || 'Solibändeli konnte nicht ausgegeben werden. Bitte kontaktiere den Betreiber.');
			actuatorTimeMs = 4000;
		}
	} catch (err) {
		console.error('Actuator request failed:', err);
		showError('Fehler beim Ausgeben des Solibändeli. Bitte versuche es erneut.');
		actuatorTimeMs = 4000;
	}
	
	setTimeout(() => {
		successBanner.classList.add('hidden');
		errorEl.textContent = '';
		errorContainer.classList.add('hidden');
		start();
	}, actuatorTimeMs);
}
