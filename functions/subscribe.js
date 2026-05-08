// Cloudflare Pages Function — handles newsletter subscriptions.
// POST /subscribe  { email: string, city: string }
// Proxies to Buttondown API so the API key stays server-side.
export async function onRequestPost(context) {
  const { request, env } = context;

  let email, city;
  try {
    ({ email, city } = await request.json());
  } catch {
    return json({ error: "invalid request body" }, 400);
  }

  if (!email || !city) {
    return json({ error: "email and city are required" }, 400);
  }

  const validCities = ["auckland", "wellington", "christchurch", "dunedin", "hamilton", "all-nz"];
  if (!validCities.includes(city)) {
    return json({ error: "invalid city" }, 400);
  }

  const apiKey = env.BUTTONDOWN_API_KEY;
  if (!apiKey) {
    return json({ error: "server misconfiguration" }, 500);
  }

  const tags = city === "all-nz"
    ? ["auckland", "wellington", "christchurch", "dunedin", "hamilton"]
    : [city];

  const res = await fetch("https://api.buttondown.email/v1/subscribers", {
    method: "POST",
    headers: {
      "Authorization": `Token ${apiKey}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ email_address: email, tags }),
  });

  if (res.status === 409) {
    // Already subscribed — treat as success so we don't leak subscriber info
    return json({ ok: true });
  }

  if (!res.ok) {
    const err = await res.text();
    console.error("Buttondown error:", res.status, err);
    return json({ error: "subscription failed" }, 502);
  }

  return json({ ok: true });
}

function json(body, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}
