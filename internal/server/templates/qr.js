// QR code rendering with multiple format support

function renderQr(data) {
	const svg = data.qr_code_svg || data.qrcode_svg || data.qr_svg || data.twint_qr_code_svg;
	const png = data.qr_code_png_base64 || data.qrcode_png_base64 || data.twint_qr_code_png_base64;
	const url = data.qr_code_url || data.qrcode_url || data.payment_qr_url || data.url;
	const qrData = data.qr || data.qrcode || data.qr_data;

	qrEl.innerHTML = '';

	// Try inline SVG first
	if (svg && typeof svg === 'string') {
		qrEl.innerHTML = svg;
		return;
	}

	// Try PNG base64
	if (png && typeof png === 'string') {
		const img = new Image();
		img.src = 'data:image/png;base64,' + png;
		img.alt = 'Payment QR code';
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
			qrEl.appendChild(img);
			return;
		}

		// Inline SVG
		if (trimmed.startsWith('<svg')) {
			qrEl.innerHTML = trimmed;
			return;
		}

		// Base64 PNG
		const base64Like = /^[A-Za-z0-9+/]+={0,2}$/;
		if (base64Like.test(trimmed)) {
			const img = new Image();
			img.src = 'data:image/png;base64,' + trimmed;
			img.alt = 'Payment QR code';
			qrEl.appendChild(img);
			return;
		}
	}

	// Try URL
	if (url && typeof url === 'string') {
		const img = new Image();
		img.src = url;
		img.alt = 'Payment QR code';
		qrEl.appendChild(img);
		return;
	}

	qrEl.textContent = 'No QR code found in the response.';
}
