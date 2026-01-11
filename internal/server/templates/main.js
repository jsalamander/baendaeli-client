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
	// Prepare sound manager and attempt to unlock on first user gesture
	document.addEventListener('click', () => Sound.enable(), { once: true });
	document.addEventListener('touchstart', () => Sound.enable(), { once: true });
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
				Sound.paymentSuccess();
				showSuccessThenRestart();
				break;
			case 'failure':
				updateStatus('Zahlung fehlgeschlagen', 'badge-error');
				Sound.paymentFailure();
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

function checkDeviceStatus() {
	fetch('/api/device/status')
		.then(res => res.json())
		.then(data => {
			const cmd = data.executing_command;
			const now = Date.now();
			const elapsedSinceDisplay = lastCommandDisplayedAt ? now - lastCommandDisplayedAt : Infinity;
			
			// Show command if it's executing or if we haven't reached 1 second yet
			if (cmd && cmd.command) {
				const commandNames = {
					'extend': 'Aktuator wird ausgefahren...',
					'retract': 'Aktuator wird eingezogen...',
					'home': 'Aktuator wird zurückgestellt...'
				};
				const displayName = commandNames[cmd.command] || `Befehl wird ausgeführt: ${cmd.command}`;
				console.log('[Device Command]', 'Starting:', cmd.command);
				deviceCommandMessage.textContent = displayName;
				deviceCommandOverlay.classList.remove('hidden');
				// Play start sound only when a new command begins
				if (lastCommandDisplayed !== cmd.command) {
					Sound.commandStart(cmd.command);
				}
				lastCommandDisplayed = cmd.command;
				lastCommandDisplayedAt = now;
			} else if (lastCommandDisplayed && elapsedSinceDisplay < 1000) {
				// Keep showing the command until 1 second has passed
				const commandNames = {
					'extend': 'Aktuator wird ausgefahren...',
					'retract': 'Aktuator wird eingezogen...',
					'home': 'Aktuator wird zurückgestellt...'
				};
				const displayName = commandNames[lastCommandDisplayed] || `Befehl wird ausgeführt: ${lastCommandDisplayed}`;
				console.log('[Device Command]', 'Keeping display for', lastCommandDisplayed, `(${1000 - elapsedSinceDisplay}ms remaining)`);
				deviceCommandMessage.textContent = displayName;
				deviceCommandOverlay.classList.remove('hidden');
			} else {
				// Command finished and 1 second has passed, clear it
				if (lastCommandDisplayed) {
					console.log('[Device Command]', 'Completed:', lastCommandDisplayed);
					Sound.commandSuccess(lastCommandDisplayed);
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

// --- Sound Manager ---
const Sound = (() => {
	let ctx = null;
	let enabled = false;
	let lastPlayed = {};
	const rateLimitMs = 250; // avoid rapid repeats

	function enable() {
		if (!enabled) {
			try {
				ctx = new (window.AudioContext || window.webkitAudioContext)();
				enabled = true;
				console.log('[Sound] Audio enabled');
			} catch (e) {
				console.warn('[Sound] Failed to init AudioContext', e);
			}
		}
	}

	function canPlay(key) {
		const now = performance.now();
		if (lastPlayed[key] && now - lastPlayed[key] < rateLimitMs) return false;
		lastPlayed[key] = now;
		return true;
	}

	function playTone(freq, durationMs = 160, type = 'sine', volume = 0.25, opts = {}) {
		if (!ctx) {
			try { enable(); } catch {}
		}
		if (!ctx) return; // give up silently if audio unavailable
		const osc = ctx.createOscillator();
		const gain = ctx.createGain();
		osc.type = type;
		osc.frequency.value = freq;
		const startTime = ctx.currentTime;
		const endTime = startTime + durationMs / 1000;

		// simple ADSR-like envelope
		gain.gain.setValueAtTime(0.0001, startTime);
		gain.gain.exponentialRampToValueAtTime(volume, startTime + 0.02);
		if (opts.rampTo) {
			osc.frequency.exponentialRampToValueAtTime(opts.rampTo, endTime);
		}
		gain.gain.exponentialRampToValueAtTime(0.0001, endTime);

		osc.connect(gain);
		gain.connect(ctx.destination);
		osc.start(startTime);
		osc.stop(endTime);
	}

	function playSequence(notes, gapMs = 40) {
		if (!ctx) { enable(); }
		if (!ctx) return;
		let t = ctx.currentTime;
		notes.forEach(n => {
			const osc = ctx.createOscillator();
			const gain = ctx.createGain();
			osc.type = n.type || 'sine';
			osc.frequency.value = n.freq;
			const dur = (n.ms || 120) / 1000;
			const vol = n.vol == null ? 0.25 : n.vol;
			gain.gain.setValueAtTime(0.0001, t);
			gain.gain.exponentialRampToValueAtTime(vol, t + 0.02);
			if (n.rampTo) {
				osc.frequency.exponentialRampToValueAtTime(n.rampTo, t + dur);
			}
			gain.gain.exponentialRampToValueAtTime(0.0001, t + dur);
			osc.connect(gain);
			gain.connect(ctx.destination);
			osc.start(t);
			osc.stop(t + dur);
			t += dur + gapMs / 1000;
		});
	}

	function paymentSuccess() {
		if (!canPlay('paymentSuccess')) return;
		// Upward triad chime: C5, E5, G5
		playSequence([
			{ freq: 523.25, ms: 120, vol: 0.22 },
			{ freq: 659.25, ms: 120, vol: 0.22 },
			{ freq: 783.99, ms: 160, vol: 0.24 }
		]);
	}

	function paymentFailure() {
		if (!canPlay('paymentFailure')) return;
		// Short descending error beep
		playTone(330, 160, 'triangle', 0.22, { rampTo: 220 });
		setTimeout(() => playTone(247, 140, 'sawtooth', 0.18, { rampTo: 196 }), 140);
	}

	function commandStart(name) {
		if (!canPlay('commandStart:' + (name || '')) ) return;
		// Quick whoosh-like rise
		playTone(300, 180, 'sine', 0.2, { rampTo: 700 });
	}

	function commandSuccess(name) {
		if (!canPlay('commandSuccess:' + (name || '')) ) return;
		// Light confirmation chime
		playSequence([
			{ freq: 587.33, ms: 110, vol: 0.22 }, // D5
			{ freq: 880.00, ms: 150, vol: 0.24 }  // A5
		]);
	}

	return {
		enable,
		paymentSuccess,
		paymentFailure,
		commandStart,
		commandSuccess,
	};
})();
