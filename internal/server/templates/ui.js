// UI state management and updates

function updateStatus(text, badgeClass = 'badge-info') {
	statusEl.innerHTML = '<span class="status status-sm status-success"></span><span>' + text + '</span>';
	statusEl.className = 'badge badge-dash ' + badgeClass;
}

function showError(message) {
	errorEl.textContent = message;
	errorContainer.classList.remove('hidden');
}

// Expiry countdown
function startExpiryCountdown(expiresAtString, validForMinutes) {
	clearExpiry();
	let target = null;
	
	if (expiresAtString) {
		const parsed = Date.parse(expiresAtString);
		if (!Number.isNaN(parsed)) {
			target = parsed;
		}
	}
	
	if (!target && validForMinutes) {
		target = Date.now() + Math.max(0, validForMinutes) * 60_000;
	}
	
	if (!target) return;
	
	expiryAt = target;
	updateExpiryCountdown();
	expiryTimer = setInterval(updateExpiryCountdown, 1000);
}

function updateExpiryCountdown() {
	if (!expiryAt) return;
	
	const remaining = expiryAt - Date.now();
	if (remaining <= 0) {
		clearExpiry();
		start();
		return;
	}
	
	const mins = Math.floor(remaining / 60_000);
	const secs = Math.floor((remaining % 60_000) / 1_000);
	expiryMeta.textContent = 'Gültig für ' + String(mins).padStart(2, '0') + ':' + String(secs).padStart(2, '0');
}

// Diagnostics state tracking
let lastDiagnosticsState = 'pending';
let lastDiagnosticsTime = null;
let lastDiagnosticsLatency = null;
let lastInternetState = 'pending';
let internetCheckInterval = null;

function setDiagnosticsPending() {
	lastDiagnosticsState = 'pending';
	lastDiagnosticsTime = null;
	lastDiagnosticsLatency = null;
	gatewayDot.className = 'w-2 h-2 rounded-full bg-warning';
	gatewayStatusText.textContent = 'Gateway wird geprüft...';
	gatewayMeta.textContent = 'Warte auf erste Antwort';
	expiryMeta.textContent = 'Gültig für --:--';
}

function updateDiagnostics({ ok, latencyMs, at }) {
	lastDiagnosticsState = ok ? 'ok' : 'bad';
	lastDiagnosticsTime = at || Date.now();
	lastDiagnosticsLatency = latencyMs != null ? Math.max(0, Math.round(latencyMs)) : null;

	if (ok) {
		gatewayDot.className = 'w-2 h-2 rounded-full bg-success';
		gatewayStatusText.textContent = 'Gateway: OK';
		const latencyText = lastDiagnosticsLatency != null ? lastDiagnosticsLatency + ' ms' : '—';
		gatewayMeta.textContent = 'Zuletzt: ' + formatTime(lastDiagnosticsTime) + ' · Latenz: ' + latencyText;
	} else {
		gatewayDot.className = 'w-2 h-2 rounded-full bg-error';
		gatewayStatusText.textContent = 'Gateway: gestört';
		gatewayMeta.textContent = 'Letzter Versuch: ' + formatTime(lastDiagnosticsTime);
	}
}

// Internet connectivity check
async function checkInternetConnection() {
	try {
		// Try to fetch a reliable endpoint with a timeout
		const controller = new AbortController();
		const timeoutId = setTimeout(() => controller.abort(), 5000);
		
		const response = await fetch('https://www.cloudflare.com/cdn-cgi/trace', {
			method: 'GET',
			cache: 'no-cache',
			signal: controller.signal
		});
		
		clearTimeout(timeoutId);
		
		if (response.ok) {
			updateInternetStatus(true);
		} else {
			updateInternetStatus(false);
		}
	} catch (error) {
		updateInternetStatus(false);
	}
}

function updateInternetStatus(isConnected) {
	lastInternetState = isConnected ? 'ok' : 'bad';
	
	if (isConnected) {
		internetDot.className = 'w-2 h-2 rounded-full bg-success';
		internetStatusText.textContent = 'Internet: OK';
	} else {
		internetDot.className = 'w-2 h-2 rounded-full bg-error';
		internetStatusText.textContent = 'Internet: Offline';
	}
}

function startInternetCheck() {
	// Check immediately
	checkInternetConnection();
	// Then check every 10 seconds
	internetCheckInterval = setInterval(checkInternetConnection, 10000);
}

function stopInternetCheck() {
	if (internetCheckInterval) {
		clearInterval(internetCheckInterval);
		internetCheckInterval = null;
	}
}

function formatTime(ts) {
	if (!ts) return '-';
	try {
		return new Date(ts).toLocaleTimeString('de-CH', { hour12: false });
	} catch {
		return '-';
	}
}
