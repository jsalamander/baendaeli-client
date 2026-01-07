// API calls and network interactions

async function safeParseJson(res) {
	const text = await res.text();
	try {
		return JSON.parse(text);
	} catch {
		return {};
	}
}

async function createPayment(amountCents) {
	const res = await fetch('/api/payment', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({
			amount_cents: amountCents,
			currency: 'CHF',
			payment_redirect_url: 'https://example.com/payments/123/complete'
		})
	});

	const data = await safeParseJson(res);

	if (!res.ok) {
		throw new Error(data.error || 'Payment creation failed');
	}

	return data;
}

async function getPaymentStatus(id, latencyMs, timestamp) {
	const res = await fetch('/api/payment/' + encodeURIComponent(id));
	const data = await safeParseJson(res);

	if (!res.ok) {
		updateDiagnostics({ ok: false, latencyMs, at: timestamp });
		throw new Error(data.error || 'Status check failed');
	}

	updateDiagnostics({ ok: true, latencyMs, at: timestamp });
	return data;
}
