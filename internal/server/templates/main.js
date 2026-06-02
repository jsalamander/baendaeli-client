// DOM Elements
const statusEl = document.getElementById('status');
const qrEl = document.getElementById('qr');
const paymentTitleEl = document.getElementById('paymentTitle');
const paymentDescriptionEl = document.getElementById('paymentDescription');
const deviceCommandOverlay = document.getElementById('deviceCommandOverlay');
const deviceCommandMessage = document.getElementById('deviceCommandMessage');

// Keep template config constants for compatibility/tests.
const defaultAmount = {{ .DefaultAmount }};
const successOverlayMs = {{ .SuccessOverlayMs }};

let deviceStatusTimer = null;

const stateUi = {
	starting: {
		status: 'Gerät startet...',
		badge: 'badge-primary',
		title: 'Gerät startet',
		description: 'Die Ausgabe wird vorbereitet. Bitte kurz warten.',
		placeholderTitle: 'Systemstart läuft',
		placeholderSubtitle: 'Bitte einen Moment Geduld.'
	},
	startup_cycle: {
		status: 'Initialzyklus läuft...',
		badge: 'badge-primary',
		title: 'Initialzyklus',
		description: 'Der erste Ausgabedurchlauf wird ausgeführt.',
		placeholderTitle: 'Initialzyklus läuft',
		placeholderSubtitle: 'Danach startet die automatische Erkennung.'
	},
	detecting_ball: {
		status: 'Warte auf Ball',
		badge: 'badge-info',
		title: 'Spende für ein Solibändeli',
		description: 'Bitte warten. Das System bereitet die Ausgabe vor und erstellt danach automatisch die Zahlung.',
		placeholderTitle: 'Bereit für nächsten Ball',
		placeholderSubtitle: 'Sobald ein Ball erkannt wird, startet die Zahlung automatisch.'
	},
	ball_detected: {
		status: 'Ball erkannt',
		badge: 'badge-warning',
		title: 'QR scannen und Betrag wählen',
		description: 'Bitte scanne den QR-Code und wähle danach den Betrag auf dem Gerät.',
		placeholderTitle: 'Zahlung wird vorbereitet',
		placeholderSubtitle: 'QR-Code wird geladen...'
	},
	awaiting_payment: {
		status: 'Warten auf Zahlung',
		badge: 'badge-warning',
		title: 'Zahlung offen',
		description: 'Bitte zahle jetzt, damit die Ausgabe gestartet wird.',
		placeholderTitle: 'Warten auf Zahlung',
		placeholderSubtitle: 'Nach erfolgreicher Zahlung wird automatisch ausgegeben.'
	},
	dispensing: {
		status: 'Ausgabe läuft',
		badge: 'badge-success',
		title: 'Ausgabe läuft',
		description: 'Dein Solibändeli wird ausgegeben.',
		placeholderTitle: 'Ausgabe läuft',
		placeholderSubtitle: 'Bitte kurz warten.'
	},
	payment_failed: {
		status: 'Zahlung fehlgeschlagen',
		badge: 'badge-error',
		title: 'Zahlung fehlgeschlagen',
		description: 'Der Vorgang wurde zurückgesetzt.',
		placeholderTitle: 'Vorgang zurückgesetzt',
		placeholderSubtitle: 'Bitte erneut versuchen.'
	},
	jam: {
		status: 'Stau erkannt',
		badge: 'badge-error',
		title: 'Technik-Hinweis',
		description: 'Bitte Techniker*in informieren.',
		placeholderTitle: 'Stau detektiert',
		placeholderSubtitle: 'Bitte rufe eine Techniker*in.'
	},
	error: {
		status: 'Fehlerzustand',
		badge: 'badge-error',
		title: 'Fehlerzustand',
		description: 'Das System meldet einen Fehler.',
		placeholderTitle: 'Fehlerzustand',
		placeholderSubtitle: 'Bitte Techniker*in informieren.'
	}
};

function mapStateUi(state) {
	return stateUi[state] || {
		status: 'Status unbekannt',
		badge: 'badge-warning',
		title: 'Status unbekannt',
		description: 'Der aktuelle Zustand konnte nicht zugeordnet werden.',
		placeholderTitle: 'Status unbekannt',
		placeholderSubtitle: 'Bitte kurz warten.'
	};
}

function updateStatus(text, badgeClass = 'badge-info') {
	statusEl.innerHTML = '<span class="status status-sm status-success"></span><span>' + text + '</span>';
	statusEl.className = 'badge badge-dash badge-outline text-base px-4 py-3 ' + badgeClass;
}

function setCommandOverlay(executingCommand, fallbackMessage) {
	if (executingCommand && executingCommand.command) {
		deviceCommandMessage.textContent = executingCommand.message || fallbackMessage || 'Aktion läuft';
		deviceCommandOverlay.classList.remove('hidden');
		return;
	}
	deviceCommandOverlay.classList.add('hidden');
}

function renderPaymentExpiry(payment) {
	if (!payment) {
		clearExpiry();
		return;
	}

	const expiresAt = payment.expires_at || payment.expiresAt || payment.expiration_at || payment.expirationAt || null;
	const validForMinutes = Number(payment.valid_for_minutes || payment.validForMinutes || 0);
	if (expiresAt || validForMinutes > 0) {
		startExpiryCountdown(expiresAt, validForMinutes);
		return;
	}

	clearExpiry();
}

function renderDeviceState(data) {
	const state = (data.state || '').toLowerCase();
	const ui = mapStateUi(state);
	const message = data.message || ui.status;
	let statusMessage = message;
	if (!data.executing_command && data.pending_command && data.pending_command.command) {
		statusMessage = message + ' · Befehl ausstehend: ' + data.pending_command.command;
	}

	updateStatus(statusMessage, ui.badge);
	paymentTitleEl.textContent = ui.title;
	paymentDescriptionEl.textContent = ui.description;
	renderPaymentExpiry(data.payment);

	if (data.payment && state === 'ball_detected') {
		renderQr(data.payment);
	} else {
		renderQrPlaceholder(ui.placeholderTitle, ui.placeholderSubtitle);
	}
	setCommandOverlay(data.executing_command, message);
}

function checkDeviceStatus() {
	const startedAt = performance.now();
	const timestamp = Date.now();

	fetch('/api/device/status')
		.then(async (res) => {
			const data = await res.json();
			if (!res.ok) {
				throw new Error('Device status unavailable');
			}

			const latencyMs = Math.round(performance.now() - startedAt);
			updateDiagnostics({ ok: true, latencyMs, at: timestamp });
			renderDeviceState(data);
		})
		.catch((err) => {
			console.error('Failed to check device status:', err);
			updateDiagnostics({ ok: false, latencyMs: null, at: Date.now() });
			updateStatus('Gerätestatus nicht verfügbar', 'badge-error');
			renderQrPlaceholder('Verbindung fehlt', 'Statusdaten konnten nicht geladen werden.');
			deviceCommandOverlay.classList.add('hidden');
		})
		.finally(() => {
			deviceStatusTimer = setTimeout(checkDeviceStatus, 1000);
		});
}

function start() {
	setDiagnosticsPending();
	startInternetCheck();
	checkDeviceStatus();
}

document.addEventListener('DOMContentLoaded', () => {
	start();
});
