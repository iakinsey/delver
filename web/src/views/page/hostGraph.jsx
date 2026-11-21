import React, { useEffect, useRef } from "react";
import Graph from "graphology";
import Sigma from "sigma";
import { EdgeArrowProgram } from "sigma/rendering";
import Layout from "graphology-layout-forceatlas2/worker";

export function HostGraph({ view, onPick, onError }) {
  const container = useRef(null),
    rings = useRef(null),
    renderer = useRef(null);
  useEffect(() => {
    if (!view || !container.current) return;
    let sigma, layout, timer, observer;
    try {
      const graph = new Graph({ type: "directed", allowSelfLoops: false });
      view.nodes.forEach((n, i) => {
        const angle = i * 2.399963229728653,
          distance = n.selected ? 0 : Math.max(1, n.depth) * 3;
        graph.addNode(n.id, {
          x: Math.cos(angle) * distance,
          y: Math.sin(angle) * distance,
          label: n.group ? n.name : shortHost(n.name),
          size: n.group ? 9 : 4 + 10 * Math.sqrt(n.percentile / 100),
          color: n.group ? "#9867d9" : n.visited ? "#267fbd" : "#9eaab6",
          data: n,
        });
      });
      view.edges.forEach((e) =>
        graph.addDirectedEdge(e.source, e.target, {
          type: "arrow",
          size: Math.min(3, 1 + Math.log2(e.links) / 3),
          color: "#8a96a777",
          label: e.links > 1 ? `${e.links} host links` : undefined,
        }),
      );
      sigma = new Sigma(graph, container.current, {
        edgeProgramClasses: { arrow: EdgeArrowProgram },
        defaultEdgeType: "arrow",
        renderEdgeLabels: false,
        labelDensity: 0.08,
        labelRenderedSizeThreshold: 9,
        labelColor: { color: "#8e9fb5" },
        hideEdgesOnMove: true,
        allowInvalidContainer: false,
      });
      renderer.current = sigma;
      sigma.on("clickNode", ({ node }) =>
        onPick(graph.getNodeAttribute(node, "data")),
      );
      const paint = () => {
        const canvas = rings.current;
        if (!canvas) return;
        const box = container.current.getBoundingClientRect(),
          ratio = window.devicePixelRatio || 1;
        if (
          canvas.width !== Math.round(box.width * ratio) ||
          canvas.height !== Math.round(box.height * ratio)
        ) {
          canvas.width = Math.round(box.width * ratio);
          canvas.height = Math.round(box.height * ratio);
        }
        const ctx = canvas.getContext("2d");
        ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
        ctx.clearRect(0, 0, box.width, box.height);
        view.nodes.forEach((n) => {
          const d = sigma.getNodeDisplayData(n.id);
          if (!d) return;
          const p = sigma.framedGraphToViewport(d);
          const size = sigma.scaleSize(d.size);
          if (!n.group && !n.visited) {
            ctx.beginPath();
            ctx.fillStyle =
              getComputedStyle(container.current).backgroundColor || "#172132";
            ctx.arc(p.x, p.y, Math.max(1, size * 0.55), 0, 2 * Math.PI);
            ctx.fill();
          }
          if (n.bridges || n.selected) {
            ctx.beginPath();
            ctx.strokeStyle = n.selected ? "#52bdff" : "#e3a326";
            ctx.lineWidth = n.selected ? 2.5 : 2;
            ctx.arc(p.x, p.y, size + 3, 0, 2 * Math.PI);
            ctx.stroke();
          }
        });
      };
      sigma.on("afterRender", paint);
      observer = new ResizeObserver(paint);
      observer.observe(container.current);
      if (graph.order > 1) {
        layout = new Layout(graph, {
          settings: {
            gravity: 0.8,
            scalingRatio: 8,
            barnesHutOptimize: graph.order > 150,
            slowDown: 5,
          },
        });
        layout.start();
        timer = setTimeout(() => layout.stop(), 1200);
      }
    } catch (error) {
      onError(`Graph rendering unavailable: ${error.message}`);
    }
    return () => {
      clearTimeout(timer);
      observer?.disconnect();
      layout?.kill();
      sigma?.kill();
      renderer.current = null;
    };
  }, [view, onPick, onError]);
  const camera = (action) => {
    const c = renderer.current?.getCamera();
    if (!c) return;
    if (action === "in") c.animatedZoom();
    else if (action === "out") c.animatedUnzoom();
    else c.animatedReset();
  };
  return (
    <>
      <div className="report-graph-controls">
        <button onClick={() => camera("in")} aria-label="Zoom in">
          ＋
        </button>
        <button onClick={() => camera("out")} aria-label="Zoom out">
          −
        </button>
        <button onClick={() => camera("reset")}>Fit</button>
      </div>
      <div
        className="report-canvas-shell"
        tabIndex={0}
        role="region"
        aria-label="Directed host graph"
        onKeyDown={(e) => {
          if (e.key === "+" || e.key === "=") {
            camera("in");
            e.preventDefault();
          } else if (e.key === "-") {
            camera("out");
            e.preventDefault();
          } else if (
            ["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(e.key)
          ) {
            const c = renderer.current?.getCamera();
            if (c) {
              const s = c.getState(),
                delta = 0.08 * s.ratio;
              c.setState({
                x:
                  s.x +
                  (e.key === "ArrowLeft"
                    ? -delta
                    : e.key === "ArrowRight"
                      ? delta
                      : 0),
                y:
                  s.y +
                  (e.key === "ArrowUp"
                    ? delta
                    : e.key === "ArrowDown"
                      ? -delta
                      : 0),
              });
              e.preventDefault();
            }
          }
        }}
      >
        <div className="report-canvas" ref={container} />
        <canvas className="report-rings" ref={rings} aria-hidden="true" />
      </div>
    </>
  );
}
export function shortHost(name) {
  return name.length > 32 ? `${name.slice(0, 15)}…${name.slice(-12)}` : name;
}
