import React from "react";
import { getNestedAttribute } from "../../util";
export function formatSeriesTime(timestamp, data, key) {
  const span =
    data.length > 1
      ? getNestedAttribute(data[data.length - 1], key) -
        getNestedAttribute(data[0], key)
      : 0;
  return new Date(timestamp * 1000).toLocaleString(
    undefined,
    span > 172800
      ? { month: "short", day: "numeric" }
      : { hour: "2-digit", minute: "2-digit" },
  );
}
export default function SeriesTable({ data, timeKey, valueKey, label }) {
  return (
    <details>
      <summary>
        {label} data ({data.length} points; latest 50 in table)
      </summary>
      <table>
        <caption>{label}</caption>
        <thead>
          <tr>
            <th scope="col">Time</th>
            <th scope="col">Value</th>
          </tr>
        </thead>
        <tbody>
          {data.slice(-50).map((point) => (
            <tr key={getNestedAttribute(point, timeKey)}>
              <th scope="row">
                {new Date(
                  getNestedAttribute(point, timeKey) * 1000,
                ).toLocaleString()}
              </th>
              <td>{getNestedAttribute(point, valueKey)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </details>
  );
}
