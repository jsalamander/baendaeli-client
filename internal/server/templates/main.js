// DOM Elements
const statusEl = document.getElementById('status');
const qrEl = document.getElementById('qr');
const errorEl = document.getElementById('error');
const errorContainer = document.getElementById('errorContainer');
const retryBtn = document.getElementById('retry');
const successBanner = document.getElementById('successBanner');
const deviceCommandOverlay = document.getElementById('deviceCommandOverlay');
const deviceCommandMessage = document.getElementById('deviceCommandMessage');
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
let deviceStatusTimer = null;
let lastCommandDisplayed = null;
let lastCommandDisplayedAt = null;
let cancelInProgress = false;

// Event Listeners
retryBtn.addEventListener('click', () => {
	retryBtn.classList.add('hidden');
	errorEl.textContent = '';
	errorContainer.classList.add('hidden');
	qrEl.innerHTML = getLoadingSpinner();
	clearPoll();
	clearExpiry();
	start();
});

// Initialization
document.addEventListener('DOMContentLoaded', () => {
	setDiagnosticsPending();
	startInternetCheck();
	startDeviceStatusCheck();
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
			currentPaymentId = data.id;
			pollStatus(data.id);
		}
		startExpiryCountdown(data.expires_at, data.valid_for_minutes);
		renderQr(data);
	} catch (err) {
		console.error('Payment creation failed', err);
		updateStatus('Etwas ist schiefgelaufen.', 'badge-error');
		
		// Show error popup with server error details
		if (err.serverError) {
			showErrorPopup(err.serverError);
		} else {
			showError('Die Zahlung konnte nicht gestartet werden. Bitte versuche es erneut.');
		}
		
		// Auto-retry after 3 seconds
		setTimeout(() => {
			start();
		}, 3000);
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

function startDeviceStatusCheck() {
	checkDeviceStatus();
}

function handleCancelCommand() {
	if (cancelInProgress) {
		return;
	}

	cancelInProgress = true;
	console.log('[Device Command]', 'Cancel current payment');
	showCancelBanner('Zahlung wurde vom Operator storniert.');
	updateStatus('Zahlung abgebrochen', 'badge-warning');
	clearPoll();
	clearExpiry();
	currentPaymentId = null;
	errorEl.textContent = '';
	errorContainer.classList.add('hidden');
	qrEl.innerHTML = getLoadingSpinner();

	setTimeout(() => {
		cancelInProgress = false;
		start();
	}, 300);
}

function checkDeviceStatus() {
	fetch('/api/device/status')
		.then(res => res.json())
		.then(data => {
			const cmd = data.executing_command;
			const now = Date.now();
			const elapsedSinceDisplay = lastCommandDisplayedAt ? now - lastCommandDisplayedAt : Infinity;
			let handledCancel = false;

			if (cmd && cmd.command === 'cancel') {
				handleCancelCommand();
				handledCancel = true;
				lastCommandDisplayed = null;
				lastCommandDisplayedAt = null;
				deviceCommandOverlay.classList.add('hidden');
			}
			
			// Show command if it's executing or if we haven't reached 1 second yet
			if (!handledCancel && cmd && cmd.command) {
				// Special handling for message command
				if (cmd.command === 'message') {
					console.log('[Device Command]', 'Message:', cmd.message);
					deviceCommandMessage.textContent = cmd.message || 'Nachricht';
					deviceCommandOverlay.classList.remove('hidden');
					lastCommandDisplayed = cmd.command;
					lastCommandDisplayedAt = now;
				} else if (cmd.command === 'load_test') {
					const progress = cmd.message ? ` (${cmd.message})` : '';
					const displayName = `Load-Test läuft...${progress}`;
					console.log('[Device Command]', 'Load test:', cmd.message || 'progress unknown');
					deviceCommandMessage.textContent = displayName;
					deviceCommandOverlay.classList.remove('hidden');
					lastCommandDisplayed = cmd.command;
					lastCommandDisplayedAt = now;
				} else {
					const commandNames = {
						'extend': 'Aktuator wird ausgefahren...',
						'retract': 'Aktuator wird eingezogen...',
						'home': 'Aktuator wird zurückgestellt...',
						'ball_dispenser': 'Ball wird ausgegeben...'
					};
					const displayName = commandNames[cmd.command] || `Befehl wird ausgeführt: ${cmd.command}`;
					console.log('[Device Command]', 'Starting:', cmd.command);
					deviceCommandMessage.textContent = displayName;
					deviceCommandOverlay.classList.remove('hidden');
					lastCommandDisplayed = cmd.command;
					lastCommandDisplayedAt = now;
				}
			} else if (lastCommandDisplayed && elapsedSinceDisplay < 1000) {
				// Keep showing the command until 1 second has passed
				if (lastCommandDisplayed === 'message') {
					// Message already displayed, just keep the overlay visible
					console.log('[Device Command]', 'Keeping message display', `(${1000 - elapsedSinceDisplay}ms remaining)`);
					deviceCommandOverlay.classList.remove('hidden');
				} else {
					const commandNames = {
						'extend': 'Aktuator wird ausgefahren...',
						'retract': 'Aktuator wird eingezogen...',
						'home': 'Aktuator wird zurückgestellt...'
					};
					const displayName = commandNames[lastCommandDisplayed] || `Befehl wird ausgeführt: ${lastCommandDisplayed}`;
					console.log('[Device Command]', 'Keeping display for', lastCommandDisplayed, `(${1000 - elapsedSinceDisplay}ms remaining)`);
					deviceCommandMessage.textContent = displayName;
					deviceCommandOverlay.classList.remove('hidden');
				}
			} else {
				// Command finished and 1 second has passed, clear it
				if (lastCommandDisplayed) {
					console.log('[Device Command]', 'Completed:', lastCommandDisplayed);
				}
				lastCommandDisplayed = null;
				lastCommandDisplayedAt = null;
				deviceCommandOverlay.classList.add('hidden');
			}
			// Schedule next check
			deviceStatusTimer = setTimeout(checkDeviceStatus, 500);
		})
		.catch(err => {
			console.error('Failed to check device status:', err);
			// Continue checking even if this fails
			deviceStatusTimer = setTimeout(checkDeviceStatus, 1000);
		});
}
