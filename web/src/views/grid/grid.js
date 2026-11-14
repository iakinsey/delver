import React from "react";
import { markSaved } from "../../event/dirty";
import GridHeader from "./header";
import GridProper from "./proper";
import { routeTo, request, hasUserInfo } from "../../util";
import { setCells, setDashboard } from "../../event/actions";
import store from "../../event/store";

export default class Grid extends React.Component {
  constructor(props) {
    super(props);

    this.state = { loading: true, error: "" };
  }

  componentDidMount() {
    this.abort = new AbortController();
    this.getDashOrReset();
  }

  componentWillUnmount() {
    this.abort.abort();
  }

  async getDashOrReset() {
    if (!hasUserInfo()) return routeTo("/login");
    const dashUuid = this.getDashUuid();

    if (!dashUuid) {
      return routeTo("/dashboards");
    }

    const args = { id: dashUuid };
    const { data, error } = await request("dashboard/load", args, {
      signal: this.abort.signal,
    });
    if (this.abort.signal.aborted) return;

    if (error || !data) {
      this.setState({ loading: false, error: error || "Dashboard not found" });
      return;
    }

    const resp = data.value;
    if (
      !resp ||
      typeof resp !== "object" ||
      (resp.cells !== undefined && !Array.isArray(resp.cells)) ||
      (resp.cells || []).some(
        (c) =>
          !c ||
          typeof c.uuid !== "string" ||
          !["chart", "metric", "map", "entityFeed"].includes(c.type),
      )
    ) {
      this.setState({
        loading: false,
        error: "Invalid dashboard configuration",
      });
      return;
    }
    const cells = (resp.cells || []).map((cell) => {
      if (cell.type !== "chart" || !cell.filter) return cell;
      const filter = JSON.parse(JSON.stringify(cell.filter));
      if (filter.query?.key) {
        filter.key = filter.query.key;
        delete filter.query.key;
      }
      if (filter.agg) {
        filter.key = filter.agg.agg_field;
        filter.fields = Array.from(
          new Set([
            ...(filter.fields || []),
            filter.key,
            filter.agg.time_field,
          ]),
        );
      }
      return { ...cell, filter };
    });

    store.dispatch(setCells(cells));
    store.dispatch(setDashboard(data));
    markSaved(store.getState().cells || {});
    this.setState({ loading: false, error: "" });
  }

  getDashUuid() {
    const tokens = window.location.pathname.split("/");

    if (tokens.length === 3) {
      return tokens[2];
    }
  }

  render() {
    if (this.state.loading)
      return <main role="status">Loading dashboard…</main>;
    if (this.state.error)
      return (
        <main>
          <p role="alert">{this.state.error}</p>
          <button onClick={() => this.getDashOrReset()}>Retry</button>
        </main>
      );
    return (
      <span>
        <GridHeader />
        <GridProper />
      </span>
    );
  }
}
