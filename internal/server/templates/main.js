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
	ball_on_sensor: {
		status: 'Ball auf Sensor erkannt',
		badge: 'badge-success',
		title: 'Ball erkannt',
		description: 'Der Ball ist am Sensor angekommen. Zahlung wird vorbereitet.',
		placeholderTitle: 'Ball erkannt',
		placeholderSubtitle: 'Die Zahlung wird jetzt erstellt.'
	},
	ball_detected: {
		status: 'Ball erkannt',
		badge: 'badge-warning',
		title: 'QR scannen und Betrag wählen',
		description: 'Bitte scanne den QR-Code und wähle danach den Betrag auf dem Gerät.',
		placeholderTitle: 'Zahlung wird vorbereitet',
		placeholderSubtitle: 'QR-Code wird geladen...'
	},
	ball_stuck_in_funnel: {
		status: 'Ball steckt im Trichter',
		badge: 'badge-error',
		title: 'Technik-Hinweis',
		description: 'Bitte Techniker*in informieren.',
		placeholderTitle: 'Ball steckt im Trichter',
		placeholderSubtitle: 'Bitte rufe eine Techniker*in.'
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

function renderPaymentExpiry(payment, state) {
	if (!payment) {
		clearExpiry();
		return;
	}

	let expiresAt = null;
	let countdownLabel = '';

	if (state === 'ball_detected') {
		expiresAt = payment.amount_selection_expires_at || payment.amount_selection_expiresAt || null;
		countdownLabel = 'Warte auf Betragsauswahl';
	} else if (state === 'awaiting_payment') {
		expiresAt = payment.payment_expires_at || payment.payment_expiresAt || null;
		countdownLabel = 'Warte auf Zahlung';
	}

	if (expiresAt && countdownLabel) {
		startExpiryCountdown(expiresAt, countdownLabel);
		return;
	}

	clearExpiry();
}

function hasAmountSelectionExpiry(payment) {
	if (!payment) {
		return false;
	}

	return Boolean(payment.amount_selection_expires_at || payment.amount_selection_expiresAt);
}

function shouldShowAmountSelectionWaiting(state, payment) {
	if (state !== 'ball_detected') {
		return false;
	}

	return hasAmountSelectionExpiry(payment);
}

function getAmountSelectionExpiryMs(payment) {
	if (!payment) {
		return null;
	}

	const raw = payment.amount_selection_expires_at || payment.amount_selection_expiresAt;
	if (!raw) {
		return null;
	}

	const parsed = Date.parse(raw);
	if (Number.isNaN(parsed)) {
		return null;
	}

	return parsed;
}

function getPaymentExpiryMs(payment) {
	if (!payment) {
		return null;
	}

	const raw = payment.payment_expires_at || payment.payment_expiresAt;
	if (!raw) {
		return null;
	}

	const parsed = Date.parse(raw);
	if (Number.isNaN(parsed)) {
		return null;
	}

	return parsed;
}

function formatRemainingLabel(expiryMs) {
	if (!expiryMs) {
		return '--:--';
	}

	const remainingMs = Math.max(0, expiryMs - Date.now());
	const totalSeconds = Math.floor(remainingMs / 1000);
	const mins = Math.floor(totalSeconds / 60);
	const secs = totalSeconds % 60;
	return String(mins).padStart(2, '0') + ':' + String(secs).padStart(2, '0');
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
	renderPaymentExpiry(data.payment, state);

	const showAmountSelectionWaiting = shouldShowAmountSelectionWaiting(state, data.payment);
	const amountSelectionExpiryMs = getAmountSelectionExpiryMs(data.payment);
	const paymentExpiryMs = getPaymentExpiryMs(data.payment);

	if (data.payment && state === 'ball_detected' && !showAmountSelectionWaiting) {
		renderQr(data.payment);
	} else if (showAmountSelectionWaiting) {
		renderQrPlaceholder('Auswahl läuft', 'Bitte im Smartphone fortfahren.');
	} else if (state === 'awaiting_payment') {
		renderQrPlaceholder('Zahlung wird abgeschlossen', 'Bitte schließe die Zahlung auf dem Smartphone ab.');
	} else {
		renderQrPlaceholder(ui.placeholderTitle, ui.placeholderSubtitle);
	}

	if (showAmountSelectionWaiting) {
		setCommandOverlay(
			{ command: 'message', message: 'Warte auf Betragsauswahl · ' + formatRemainingLabel(amountSelectionExpiryMs) },
			message,
		);
		return;
	}

	if (state === 'awaiting_payment') {
		setCommandOverlay(
			{ command: 'message', message: 'Warte auf Zahlung · ' + formatRemainingLabel(paymentExpiryMs) },
			message,
		);
		return;
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
