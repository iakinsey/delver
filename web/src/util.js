import { API_SERVER } from "./config";

export function uuid4() {
  return ([1e7] + -1e3 + -4e3 + -8e3 + -1e11).replace(/[018]/g, (c) =>
    // eslint-disable-next-line
    (
      c ^
      (crypto.getRandomValues(new Uint8Array(1))[0] & (15 >> (c / 4)))
    ).toString(16),
  );
}

export function routeTo(url) {
  window.location.href = url;
}

export async function request(url, data, options = {}) {
  const requestUrl = `${API_SERVER}/${url}`;
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 15000);
  const forwardAbort = () => controller.abort();
  if (options.signal?.aborted) controller.abort();
  options.signal?.addEventListener("abort", forwardAbort, { once: true });

  try {
    const response = await fetch(requestUrl, {
      method: "POST",
      signal: controller.signal,
      body: JSON.stringify(data),
      headers: {
        "Content-Type": "application/json",
      },
    });

    const result = await response.json();
    return result;
  } catch (error) {
    return {
      success: false,
      error: "Unable to reach the server. Please retry.",
    };
  } finally {
    clearTimeout(timeout);
    options.signal?.removeEventListener("abort", forwardAbort);
  }
}
