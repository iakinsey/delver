export function rebuildFilter(base, fields, values) {
  const result = JSON.parse(JSON.stringify(base));
  for (const [key, field] of Object.entries(fields)) {
    if (result.query && key in result.query) {
      if (Array.isArray(result.query[key])) result.query[key] = [];
      else delete result.query[key];
    }
    const value =
      typeof values[key] === "string" ? values[key].trim() : values[key];
    if (field.required && (value === "" || value == null))
      throw Error(`${field.label} is required`);
    if (
      key === "http_code" &&
      value &&
      String(value)
        .split(/\s+/)
        .some((v) => !/^\d{3}$/.test(v) || Number(v) < 100 || Number(v) > 599)
    )
      throw Error("HTTP codes must be numbers from 100 to 599");
    if (
      value !== "" &&
      field.type === "number" &&
      (!Number.isFinite(Number(value)) || Number(value) <= 0)
    )
      throw Error("Enter a positive number");
    if (value && field.type === "datetime") {
      const dates = value.split("|");
      if (
        dates.length !== 2 ||
        dates.some((date) => !Number.isFinite(Date.parse(date))) ||
        Date.parse(dates[0]) > Date.parse(dates[1])
      )
        throw Error("Enter a valid start and end date in chronological order");
    }
    if (field.type === "datetime" && !value && result.query) {
      delete result.query.start;
      delete result.query.end;
    }
    if (value !== "" && value !== undefined && value !== null) {
      const args =
        field.type === "datetime"
          ? value.split("|")
          : [field.type === "number" ? Number(value) : value];
      field.onUpdate?.(result, key, args);
    }
  }
  if (result.fields) result.fields = [...new Set(result.fields)];
  return result;
}
