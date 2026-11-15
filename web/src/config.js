export const API_SERVER =
  process.env.REACT_APP_API_SERVER ||
  (["5173", "15173"].includes(window.location.port)
    ? `${window.location.protocol}//${window.location.hostname}:8080`
    : window.location.origin);
