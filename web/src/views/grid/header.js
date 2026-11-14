import React from "react";
import { Prompt } from "react-router-dom";
import store from "../../event/store";
import { setCell } from "../../event/actions";
import { getCells, getDashboard } from "../../event/selectors";
import { request, uuid4 } from "../../util";
import { isDirty, markSaved, takeRemoved } from "../../event/dirty";
export default class GridHeader extends React.Component {
  state = {
    configOn: false,
    status: "",
    saving: false,
    undo: false,
    revision: 0,
  };
  componentDidMount() {
    this.unsub = store.subscribe(() =>
      this.setState((s) => ({
        revision: s.revision + 1,
        status: this.state.saving ? s.status : "",
      })),
    );
    this.removed = () => this.setState({ undo: true });
    window.addEventListener("widget-removed", this.removed);
    this.guard = (e) => {
      if (isDirty(getCells(store.getState()))) {
        e.preventDefault();
        e.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", this.guard);
  }
  componentWillUnmount() {
    this.unsub();
    window.removeEventListener("widget-removed", this.removed);
    window.removeEventListener("beforeunload", this.guard);
  }
  async save() {
    this.setState({ saving: true, status: "Saving…", saveError: "" });
    const state = store.getState(),
      dash = getDashboard(state),
      cells = getCells(state);
    const result = await request("dashboard/save", {
      id: dash.id,
      name: dash.name,
      description: dash.description,
      value: { cells: Object.values(cells) },
    });
    if (!result.error) markSaved(cells);
    this.setState({
      saving: false,
      saveError: result.error || "",
      status: result.error ? "" : "Dashboard saved",
    });
  }
  render() {
    const state = store.getState(),
      cells = getCells(state),
      dash = getDashboard(state);
    return (
      <section className="dashboard-toolbar">
        <Prompt
          when={isDirty(cells)}
          message="You have unsaved dashboard changes. Leave this page?"
        />
        <h1 className="sr-only">{dash.name || "Dashboard"}</h1>
        <div className="classic-grid-actions">
          <button
            className="icon-button"
            aria-label="Add widget"
            title="Add widget"
            onClick={() => this.setState({ configOn: !this.state.configOn })}
          >
            <img src="/add.png" alt="" />
          </button>
          <button
            className="icon-button"
            aria-label="Save dashboard"
            title="Save dashboard"
            disabled={this.state.saving || !dash.id}
            onClick={() => this.save()}
          >
            <img src="/save.png" alt="" />
          </button>
        </div>
        {this.state.configOn && (
          <div className="classic-add-options">
            {Object.entries({
              entityFeed: "Feed",
              chart: "Chart",
              map: "Map",
              metric: "Metric",
            }).map(([type, label]) => (
              <button
                key={type}
                onClick={() => {
                  store.dispatch(
                    setCell({
                      uuid: uuid4(),
                      type,
                      title: label,
                      attributes: { x: 0, y: 0, w: 32, h: 8 },
                    }),
                  );
                  this.setState({ configOn: false });
                }}
              >
                {label}
              </button>
            ))}
          </div>
        )}
        {this.state.undo && (
          <button
            onClick={() => {
              const cell = takeRemoved();
              if (cell) store.dispatch(setCell(cell));
              this.setState({ undo: false });
            }}
          >
            Undo removal
          </button>
        )}
        <span
          role="status"
          className={!this.state.status && !isDirty(cells) ? "sr-only" : ""}
        >
          {this.state.saveError ||
            (this.state.saving
              ? "Saving…"
              : isDirty(cells)
                ? "Unsaved changes"
                : this.state.status || "All changes saved")}
        </span>
        {!Object.keys(cells).length && (
          <p>
            Add a widget to begin. Configure its data, apply settings, then save
            this dashboard.
          </p>
        )}
      </section>
    );
  }
}
