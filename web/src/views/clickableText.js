import React from "react";
export default function ClickableText({ value, onChange }) {
  const [editing, setEditing] = React.useState(false);
  const [draft, setDraft] = React.useState(value);
  return editing ? (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        onChange(draft.trim() || "Untitled widget");
        setEditing(false);
      }}
    >
      <label>
        Widget title{" "}
        <input
          autoFocus
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
        />
      </label>
      <button>Apply title</button>
      <button type="button" onClick={() => setEditing(false)}>
        Cancel
      </button>
    </form>
  ) : (
    <button
      onClick={() => {
        setDraft(value);
        setEditing(true);
      }}
      className="widget-title" aria-label={`Rename ${value}`}
    >
      {value}
    </button>
  );
}
