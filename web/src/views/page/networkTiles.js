import { useEffect, useRef, useState } from "react";

export function tileRequests(camera, width, height) {
  if (!(width > 0 && height > 0)) return [];
  const unit = Math.min(width, height) * camera.zoom;
  let z = Math.max(0, Math.min(16, Math.floor(Math.log2(unit / 256))));
  const at = (level) => {
    const grid = 2 ** level;
    const left = Math.max(0, Math.floor((camera.x - width / unit / 2) * grid));
    const top = Math.max(0, Math.floor((camera.y - height / unit / 2) * grid));
    const right = Math.min(
      grid - 1,
      Math.floor((camera.x + width / unit / 2) * grid),
    );
    const bottom = Math.min(
      grid - 1,
      Math.floor((camera.y + height / unit / 2) * grid),
    );
    return {
      left,
      top,
      right,
      bottom,
      count: Math.max(0, right - left + 1) * Math.max(0, bottom - top + 1),
    };
  };
  while (z > 0 && at(z).count > 36) z--;
  const { left, top, right, bottom } = at(z);
  const requests = [{ z: 0, x: 0, y: 0 }];
  for (let x = left; x <= right; x++)
    for (let y = top; y <= bottom; y++)
      if (z || x || y) requests.push({ z, x, y });
  return requests;
}

export function tileGeometry(tile, camera, width, height) {
  const unit = Math.min(width, height) * camera.zoom;
  const grid = 2 ** tile.z;
  return {
    x: (tile.x / grid - camera.x) * unit + width / 2,
    y: (tile.y / grid - camera.y) * unit + height / 2,
    size: unit / grid,
  };
}

// Only decoded tiles near the camera are retained. Geometry is immutable: old
// tiles transform immediately during a gesture while finer pixels load.
export function useNetworkTiles(
  call,
  snapshot,
  camera,
  size,
  selected,
  reconnect,
) {
  const cache = useRef(new Map());
  const [version, setVersion] = useState(0),
    [error, setError] = useState("");
  const id = snapshot?.id,
    format = snapshot?.format;
  useEffect(() => {
    cache.current.clear();
    setError("");
  }, [id]);
  useEffect(() => {
    if (!id || format !== 2) return;
    const ctrl = new AbortController();
    const timer = setTimeout(() => {
      const requests = tileRequests(camera, size.width, size.height);
      const jobs = [];
      for (const tile of requests) {
        // A neutral overview remains available when changing the selection.
        for (const host of tile.z === 0 && selected
          ? [0, selected]
          : [selected || 0]) {
          const key = `${id}:${tile.z}:${tile.x}:${tile.y}:${host}`;
          const previous = cache.current.get(key);
          if (previous) {
            cache.current.delete(key);
            cache.current.set(key, previous);
          } else jobs.push({ ...tile, host, key });
        }
      }
      let cursor = 0;
      async function worker() {
        while (!ctrl.signal.aborted && cursor < jobs.length) {
          const job = jobs[cursor++];
          try {
            const data = await call(
              "tile",
              {
                snapshot: id,
                z: job.z,
                x: job.x,
                y: job.y,
                selected_host: job.host,
              },
              ctrl.signal,
            );
            if (ctrl.signal.aborted) return;
            const image = await new Promise((resolve, reject) => {
              const img = new Image();
              img.onload = () => resolve(img);
              img.onerror = () => reject(Error("Graph tile decoding failed"));
              img.src = `data:image/png;base64,${data.png}`;
            });
            if (ctrl.signal.aborted) return;
            cache.current.set(job.key, { ...job, image, snapshot: id });
            while (cache.current.size > 48) {
              const key = cache.current.keys().next().value;
              cache.current.delete(key);
            }
            setVersion((v) => v + 1);
            setError("");
          } catch (e) {
            if (!ctrl.signal.aborted) setError(e.message);
          }
        }
      }
      for (let i = 0; i < 3; i++) worker();
    }, 100);
    return () => {
      clearTimeout(timer);
      ctrl.abort();
    };
  }, [call, id, format, camera, size, selected, reconnect]);
  const tiles = selectDrawTiles(
    cache.current.values(),
    id,
    selected,
    camera,
    size.width,
    size.height,
  );
  void version;
  return { tiles, error };
}

export function selectDrawTiles(values, id, selected, camera, width, height) {
  const maxLevel = Math.max(
    0,
    ...tileRequests(camera, width, height).map((t) => t.z),
  );
  const tiles = [...values]
    .filter(
      (t) =>
        t.snapshot === id &&
        t.z <= maxLevel &&
        (t.host === 0 || t.host === (selected || 0)),
    )
    .sort((a, b) => a.z - b.z || a.host - b.host);
  return tiles;
}
