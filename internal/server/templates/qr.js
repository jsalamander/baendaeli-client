// QR code rendering with multiple format support

let qrResizeRaf = null;
let qrResizeTimer = null;
const MIN_QR_SIZE = 96;
const MAX_QR_SIZE = 360;

function getLayoutOverflowPx() {
	const viewportHeight = window.innerHeight || document.documentElement.clientHeight || 0;
	const paymentCardEl = document.getElementById('paymentCard');
	if (!paymentCardEl) {
		return 0;
	}

	const cardBottom = paymentCardEl.getBoundingClientRect().bottom;
	return Math.max(0, Math.ceil(cardBottom - viewportHeight));
}

function updateBottomSafeArea() {
	const viewportHeight = window.innerHeight || document.documentElement.clientHeight || 0;
	const root = document.documentElement;
	const versionBadgeEl = document.getElementById('versionBadge');
	const diagnosticsBadgesEl = document.getElementById('diagnosticsBadges');

	const occupiedFromBottom = (el) => {
		if (!el) {
			return 0;
		}
		const rect = el.getBoundingClientRect();
		return Math.max(0, viewportHeight - rect.top);
	};

	const reservedBottom = Math.max(40, occupiedFromBottom(versionBadgeEl), occupiedFromBottom(diagnosticsBadgesEl));
	root.style.setProperty('--bottom-safe-area', Math.round(reservedBottom + 6) + 'px');
}

function updateResponsiveQrSize() {
	if (!qrEl) {
		return;
	}

	updateBottomSafeArea();

	const viewportWidth = window.innerWidth || document.documentElement.clientWidth || 0;
	const viewportHeight = window.innerHeight || document.documentElement.clientHeight || 0;
	const mainEl = document.getElementById('mainContent');
	const mainStyles = mainEl ? window.getComputedStyle(mainEl) : null;
	const mainPadLeft = mainStyles ? parseFloat(mainStyles.paddingLeft) || 0 : 0;
	const mainPadRight = mainStyles ? parseFloat(mainStyles.paddingRight) || 0 : 0;
	const qrPadding = parseFloat(window.getComputedStyle(qrEl).paddingLeft) || 0;

	const widthBudget = Math.max(0, viewportWidth - mainPadLeft - mainPadRight);
	const maxByCardWidth = Math.max(0, widthBudget - 224); // 14rem card side content budget
	const maxByQrBoxWidth = Math.max(0, widthBudget - (qrPadding * 2) - 24);
	const initialWidthCap = Math.min(maxByCardWidth, maxByQrBoxWidth, viewportWidth * 0.85);
	const initialHeightCap = viewportHeight * 0.62;

	let qrSize = Math.round(Math.max(MIN_QR_SIZE, Math.min(MAX_QR_SIZE, initialWidthCap, initialHeightCap)));
	if (!Number.isFinite(qrSize) || qrSize <= 0) {
		qrSize = MIN_QR_SIZE;
	}

	document.documentElement.style.setProperty('--qr-size', qrSize + 'px');

	for (let i = 0; i < 50 && qrSize > MIN_QR_SIZE; i += 1) {
		if (getLayoutOverflowPx() <= 0) {
			break;
		}
		qrSize = Math.max(MIN_QR_SIZE, qrSize - 4);
		document.documentElement.style.setProperty('--qr-size', qrSize + 'px');
	}

	document.body.classList.toggle('compact-layout', getLayoutOverflowPx() > 0);
}

function scheduleResponsiveQrSizeUpdate() {
	if (qrResizeRaf !== null) {
		cancelAnimationFrame(qrResizeRaf);
	}
	if (qrResizeTimer !== null) {
		clearTimeout(qrResizeTimer);
		qrResizeTimer = null;
	}

	qrResizeRaf = requestAnimationFrame(() => {
		updateResponsiveQrSize();
		qrResizeRaf = null;
	});
}

function scheduleResponsiveQrSizeUpdateWithDelay(delayMs) {
	if (qrResizeTimer !== null) {
		clearTimeout(qrResizeTimer);
	}
	qrResizeTimer = setTimeout(() => {
		scheduleResponsiveQrSizeUpdate();
		qrResizeTimer = null;
	}, delayMs);
}

window.addEventListener('resize', scheduleResponsiveQrSizeUpdate, { passive: true });
window.addEventListener('orientationchange', () => {
	scheduleResponsiveQrSizeUpdate();
	scheduleResponsiveQrSizeUpdateWithDelay(120);
	scheduleResponsiveQrSizeUpdateWithDelay(320);
}, { passive: true });
window.addEventListener('pageshow', scheduleResponsiveQrSizeUpdate, { passive: true });

if (window.visualViewport) {
	window.visualViewport.addEventListener('resize', scheduleResponsiveQrSizeUpdate, { passive: true });
}

document.addEventListener('DOMContentLoaded', scheduleResponsiveQrSizeUpdate);

function renderQr(data) {
	const svg = data.qr_code_svg || data.qrcode_svg || data.qr_svg || data.twint_qr_code_svg;
	const png = data.qr_code_png_base64 || data.qrcode_png_base64 || data.twint_qr_code_png_base64;
	const url = data.qr_code_url || data.qrcode_url || data.payment_qr_url || data.url;
	const qrData = data.qr || data.qrcode || data.qr_data;

	const applyImageSizing = (img) => {
		img.className = 'mx-auto block h-auto max-w-full';
		img.style.aspectRatio = '1 / 1';
		img.style.objectFit = 'contain';
		img.style.width = 'var(--qr-size)';
		img.style.height = 'var(--qr-size)';
	};

	const applyInlineSvgSizing = () => {
		const svgEl = qrEl.querySelector('svg');
		if (!svgEl) {
			return;
		}
		svgEl.style.display = 'block';
		svgEl.style.margin = '0 auto';
		svgEl.style.maxWidth = '100%';
		svgEl.style.width = 'var(--qr-size)';
		svgEl.style.height = 'var(--qr-size)';
		svgEl.style.aspectRatio = '1 / 1';
	};

	scheduleResponsiveQrSizeUpdate();

	qrEl.innerHTML = '';

	// Try inline SVG first
	if (svg && typeof svg === 'string') {
		qrEl.innerHTML = svg;
		applyInlineSvgSizing();
		return;
	}

	// Try PNG base64
	if (png && typeof png === 'string') {
		const img = new Image();
		img.src = 'data:image/png;base64,' + png;
		img.alt = 'Payment QR code';
		applyImageSizing(img);
		qrEl.appendChild(img);
		return;
	}

	// Try QR data in various formats
	if (qrData && typeof qrData === 'string') {
		const trimmed = qrData.trim();
		
		// Data URL format
		if (trimmed.startsWith('data:')) {
			const img = new Image();
			img.src = trimmed;
			img.alt = 'Payment QR code';
			applyImageSizing(img);
			qrEl.appendChild(img);
			return;
		}

		// Inline SVG
		if (trimmed.startsWith('<svg')) {
			qrEl.innerHTML = trimmed;
			applyInlineSvgSizing();
			return;
		}

		// Base64 PNG
		const base64Like = /^[A-Za-z0-9+/]+={0,2}$/;
		if (base64Like.test(trimmed)) {
			const img = new Image();
			img.src = 'data:image/png;base64,' + trimmed;
			img.alt = 'Payment QR code';
			applyImageSizing(img);
			qrEl.appendChild(img);
			return;
		}
	}

	// Try URL
	if (url && typeof url === 'string') {
		const img = new Image();
		img.src = url;
		img.alt = 'Payment QR code';
		applyImageSizing(img);
		qrEl.appendChild(img);
		return;
	}

	qrEl.textContent = 'No QR code found in the response.';
}
