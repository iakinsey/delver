import React from "react";

export default function Header() {
  return (
    <header style={{ height: "60px", display: "block" }}>
      <a
        href="/"
        style={{
          position: "absolute",
          fontSize: "3em",
          top: 12,
          left: 12,
          color: "#8282a7",
        }}
      >
        Delver
      </a>
    </header>
  );
}
