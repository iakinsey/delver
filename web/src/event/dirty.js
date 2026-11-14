let saved = "";
let removed = null;
export const markSaved = (cells) => {
  saved = JSON.stringify(cells);
};
export const isDirty = (cells) => JSON.stringify(cells) !== saved;
export const rememberRemoved = (cell) => {
  removed = cell;
  window.dispatchEvent(new Event("widget-removed"));
};
export const takeRemoved = () => {
  const cell = removed;
  removed = null;
  return cell;
};
