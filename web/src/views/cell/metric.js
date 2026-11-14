import React from "react";
import SeriesTable, { formatSeriesTime } from "./seriesTable";
import Connectable from "../connectable";
import QueryBuilder from "../query";
import { getDateQueryString } from "../../util";
import {
  LineChart,
  Line,
  CartesianGrid,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
} from "recharts";
import { mergeSeries } from "../../util";

const DEFAULT_QUERY = {
  data_type: "metric",
  query: {
    key: "httpFetcher.message.in",
  },
  options: {
    preload: true,
  },
};

export default class MetricView extends Connectable {
  constructor(props) {
    super(props);

    this.cell = props.cell;
    const filter = props.cell.filter || DEFAULT_QUERY;
    const filterJSON = JSON.stringify(filter);
    const chart = { data: [] };

    if (filter.title) {
      chart.label = filter.title;
    }
    this.state = {
      filter: filterJSON,
      filterInProgress: filterJSON,
      configOn: this.cell.filter ? false : true,
      title: this.cell.title ? this.cell.title : "",
      err: "",
      chart: chart,
      data: [],
    };
  }

  onMessage(message, reconnect) {
    this.setState((state) => ({
      data: mergeSeries(state.data, message.data, "when"),
    }));
  }

  renderQueryBuilder() {
    const fields = {
      key: {
        label: "Metric key",
        required: true,
        type: "text",
        operators: ["equal"],
        getter: (filter, key) => filter.query[key],
        onUpdate: (filter, key, value) => (filter.query[key] = value[0]),
      },
      range: {
        label: "Range",
        type: "datetime",
        operators: ["between"],
        getter: (filter, key) => [
          getDateQueryString(filter.query.start),
          getDateQueryString(filter.query.end),
        ],
        onUpdate: (filter, key, value) => {
          filter.query.start = new Date(value[0]).getTime() / 1000;
          filter.query.end = new Date(value[1]).getTime() / 1000;
        },
      },
      preload: {
        label: "Preload",
        type: "boolean",
        operators: ["equal"],
        defaultValue: true,
        getter: (filter, key) => filter.options[key],
        onUpdate: (filter, key, value) => (filter.options[key] = value[0]),
      },
    };

    return (
      <QueryBuilder
        filter={this.state.filter}
        fields={fields}
        onError={(msg) => this.setState({ err: msg })}
        onUpdate={(d) =>
          this.setState({
            filterInProgress: JSON.stringify(d),
            err: undefined,
          })
        }
      />
    );
  }

  renderMetric() {
    if (!this.cell.filter) {
      return;
    }

    return (
      <div className="chart-body"><ResponsiveContainer width="99%" height="100%">
        <LineChart
          accessibilityLayer
          data={this.state.data}
          margin={{ top: 5, right: 5, bottom: 5 }}
        >
          <Line type="monotone" dataKey="value" stroke="#8884d8" dot={false} />
          <CartesianGrid stroke="#ccc" strokeDasharray="5 5" />
          <XAxis
            dataKey="when"
            tickFormatter={(t) => formatSeriesTime(t, this.state.data, "when")}
          />
          <Tooltip
            labelFormatter={(t) => new Date(t * 1000).toLocaleString()}
          />
          <YAxis
            label={{
              value: "Value",
              angle: -90,
              position: "insideLeft",
              fill: "#c5d1e3",
            }}
          />
        </LineChart>
      </ResponsiveContainer></div>
    );
  }

  render() {
    return (
      <section className="widget" data-widget-id={this.props.cell.uuid}>
        {this.renderConfig()}
        {this.renderMetric()}
        <SeriesTable
          data={this.state.data}
          timeKey="when"
          valueKey={"value"}
          label={this.props.cell.title || "Series"}
        />
      </section>
    );
  }
}
