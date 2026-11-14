import React from "react";
import SeriesTable, { formatSeriesTime } from "./seriesTable";
import Connectable from "../connectable";
import {
  LineChart,
  Line,
  CartesianGrid,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
} from "recharts";
import QueryBuilder from "../query";
import { mergeSeries } from "../../util";

const TIMESTAMP_KEY = "timestamp";
const DEFAULT_QUERY = {
  fields: ["features.sentiment.binary_naive_bayes_title", "timestamp"],
  key: "features.sentiment.binary_naive_bayes_title",
  title: "Sentiment",
  data_type: "composite",
  query: {
    keyword: [],
    country: [],
    company: [],
  },
  agg: {
    time_field: "timestamp",
    agg_field: "features.sentiment.binary_naive_bayes_title",
    time_window_seconds: 1800,
    agg_name: "avg",
  },
  options: {
    preload: true,
  },
};

export default class _Chart extends Connectable {
  constructor(props) {
    super(props);

    this.cell = props.cell;
    const filter = JSON.parse(
      JSON.stringify(props.cell.filter || DEFAULT_QUERY),
    );
    if (filter.query?.key) {
      filter.key = filter.query.key;
      delete filter.query.key;
    }
    if (filter.agg) {
      filter.key = filter.agg.agg_field;
      filter.fields = Array.from(
        new Set([...(filter.fields || []), filter.key, filter.agg.time_field]),
      );
    }
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

  onMessage(message) {
    this.setState((state) => ({
      data: mergeSeries(state.data, message.data, "timestamp"),
    }));
  }

  renderChart() {
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
          <Line
            isAnimationActive={false}
            type="monotone"
            dataKey={this.cell.filter.key}
            stroke="#8884d8"
            strokeWidth={3}
            dot={false}
          />
          <CartesianGrid stroke="#ccc" strokeDasharray="5 5" />
          <XAxis
            dataKey={TIMESTAMP_KEY}
            tickFormatter={(t) =>
              formatSeriesTime(t, this.state.data, "timestamp")
            }
          />
          <Tooltip
            labelFormatter={(t) => new Date(t * 1000).toLocaleString()}
          />
          <YAxis
            label={{
              value: (this.cell.filter.key || " ").startsWith(
                "features.sentiment.",
              )
                ? "Sentiment (0–1)"
                : this.cell.filter.key,
              angle: -90,
              position: "insideLeft",
            }}
            domain={
              (this.cell.filter.key || "").startsWith("features.sentiment.")
                ? [0, 1]
                : ["auto", "auto"]
            }
          />
        </LineChart>
      </ResponsiveContainer></div>
    );
  }

  renderQueryBuilder() {
    const fields = {
      key: {
        label: "Plotted field",
        type: "text",
        required: true,
        operators: ["equal"],
        getter: (filter, key) => filter.key,
        onUpdate: (filter, key, value) => {
          filter.key = value[0];
          if (filter.agg) filter.agg.agg_field = value[0];
          filter.fields = Array.from(
            new Set([...(filter.fields || []), value[0]]),
          );
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
      time_field: {
        label: "Time Field",
        required: true,
        type: "text",
        operators: ["equal"],
        defaultValue: "found",
        getter: (filter, key) => filter.agg.time_field,
        onUpdate: (filter, key, value) => {
          filter.agg.time_field = value[0];
          filter.fields.push(value[0]);
        },
      },
      keyword: {
        label: "Keywords",
        type: "text",
        operators: ["equal"],
        getter: (filter, key) => filter.query.keyword.join(" "),
        onUpdate: (filter, key, value) =>
          (filter.query.keyword = value[0].trim().split(/\s+/).filter(Boolean)),
      },
      country: {
        label: "Countries (ISO 3166-1 alpha-3)",
        type: "text",
        operators: ["equal"],
        getter: (filter, key) => filter.query.country.join(" "),
        onUpdate: (filter, key, value) =>
          (filter.query.country = value[0].trim().split(/\s+/).filter(Boolean)),
      },
      company: {
        label: "Company (Exchange:Ticker)",
        type: "text",
        operators: ["equal"],
        getter: (filter, key) => filter.query.company.join(" "),
        onUpdate: (filter, key, value) =>
          (filter.query.company = value[0].trim().split(/\s+/).filter(Boolean)),
      },
      time_window: {
        label: "Aggregate Time Window (Seconds)",
        required: true,
        type: "number",
        operators: ["equal"],
        defaultValue: 1800,
        getter: (filter, key) => filter.agg.time_window_seconds,
        onUpdate: (filter, key, value) =>
          (filter.agg.time_window_seconds = value[0]),
      },
    };

    if (!JSON.parse(this.state.filter).agg) {
      delete fields.time_field;
      delete fields.time_window;
    }
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

  render() {
    return (
      <section className="widget" data-widget-id={this.props.cell.uuid}>
        {this.renderConfig()}
        {this.renderChart()}
        <SeriesTable
          data={this.state.data}
          timeKey="timestamp"
          valueKey={this.props.cell.filter?.key || "value"}
          label={this.props.cell.title || "Series"}
        />
      </section>
    );
  }
}
