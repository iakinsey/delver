import md5 from "md5";
import { API_SERVER } from "./config";

export function validateJSON(str) {
  try {
    JSON.parse(str);
  } catch (e) {
    return String(e).split(":").pop(-1).trim();
  }
}

export function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

export function uuid4() {
  return ([1e7] + -1e3 + -4e3 + -8e3 + -1e11).replace(/[018]/g, (c) =>
    // eslint-disable-next-line
    (
      c ^
      (crypto.getRandomValues(new Uint8Array(1))[0] & (15 >> (c / 4)))
    ).toString(16),
  );
}

export function getKey(prefix, dictionary) {
  return md5(prefix + JSON.stringify(dictionary));
}

export function routeTo(url) {
  window.location.href = url;
}

export function getUserInfo() {
  return {
    email: localStorage.email,
    ...(localStorage.auth && !["user/create", "user/authenticate"].includes(url)
      ? { auth: localStorage.auth }
      : {}),
  };
}
export function setUserInfo(email, auth) {
  localStorage.email = email;
  localStorage.auth = auth;
}

export function hasUserInfo() {
  return localStorage.email && localStorage.auth;
}

export async function request(url, data, options = {}) {
  if (!hasUserInfo() && !["user/create", "user/authenticate"].includes(url)) {
    routeTo("/login");
    return { success: false, error: "Authentication required" };
  }

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
        ...(localStorage.auth &&
        !["user/create", "user/authenticate"].includes(url)
          ? { auth: localStorage.auth }
          : {}),
      },
    });

    const result = await response.json();
    if (
      response.status === 401 ||
      (result.http_code === 2 &&
        !["user/create", "user/authenticate"].includes(url)) ||
      /session expired|invalid token/i.test(result.error || "")
    )
      window.dispatchEvent(new Event("session-expired"));
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

export async function logout() {
  await request("user/logout", {});
  delete localStorage.email;
  delete localStorage.auth;

  routeTo("/");
}

export function getDateQueryString(d) {
  if (!(d instanceof Date)) d = new Date(Number(d) * 1000);
  const datePart = [
    d.getFullYear(),
    String(d.getMonth() + 1).padStart(2, "0"),
    String(d.getDate()).padStart(2, "0"),
  ].join("-");

  const timePart = [
    String(d.getHours()).padStart(2, "0"),
    String(d.getMinutes()).padStart(2, "0"),
    String(d.getSeconds()).padStart(2, "0"),
  ].join(":");

  return `${datePart} ${timePart}`;
}

export function getNestedAttribute(obj, path) {
  if (!obj || typeof path !== "string") return undefined;
  var pos = 0,
    end,
    val = obj;
  while ((end = path.indexOf(".", pos)) !== -1) {
    if (val == null) return undefined;
    val = val[path.substring(pos, end)];
    if (val === undefined) return undefined;
    pos = end + 1;
  }
  return val == null ? undefined : val[path.substr(pos)];
}

export function mergeSeries(previous, incoming, timeKey = "timestamp") {
  const points = new Map(
    previous.map((point) => [getNestedAttribute(point, timeKey), point]),
  );
  for (const point of incoming)
    points.set(getNestedAttribute(point, timeKey), point);
  return Array.from(points.values())
    .sort(
      (a, b) => getNestedAttribute(a, timeKey) - getNestedAttribute(b, timeKey),
    )
    .slice(-5000);
}
