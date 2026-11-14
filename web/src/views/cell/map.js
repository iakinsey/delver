import React from "react";
import Connectable from "../connectable";
import { scaleLinear } from "d3-scale";
import { getNestedAttribute } from "../../util.js";
import QueryBuilder from "../query";
import { ComposableMap, Geographies, Geography } from "react-simple-maps";

const DEFAULT_COLOR = "#323232";
const GEO_URL = "/world-110m.json";
const SENTIMENT_KEY = "features.sentiment.binary_naive_bayes_title";
const COUNTRY_KEY = "features.country";
const TIMESTAMP_KEY = "timestamp";
const DEFAULT_QUERY = {
  fields: [SENTIMENT_KEY, TIMESTAMP_KEY, COUNTRY_KEY, "request_id"],
  key: SENTIMENT_KEY,
  title: "Sentiment",
  data_type: "composite",
  query: {
    keyword: [],
    country: [],
    company: [],
  },
  options: {
    preload: true,
  },
};

const colorScale = scaleLinear()
  .domain([0, 0.5, 1])
  .range(["#cb0000", "#ffeb00", "#4d9508"]);

export default class Map extends Connectable {
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
      title: this.cell.title ? this.cell.title : "",
      configOn: this.cell.filter ? false : true,
      err: "",
      chart: chart,
    };

    this.defaultLen = Object.keys(this.state).length;
    this.seen = new Set();
  }

  onMessage(message) {
    this.setState((state) => {
      const updates = {};
      for (const entity of message.data) {
        const countries = getNestedAttribute(entity, COUNTRY_KEY);
        const sentiment = getNestedAttribute(entity, SENTIMENT_KEY);
        if (
          !countries ||
          typeof sentiment !== "number" ||
          !Number.isFinite(sentiment)
        )
          continue;
        if (entity.request_id && this.seen.has(entity.request_id)) continue;
        if (entity.request_id) this.seen.add(entity.request_id);
        if (this.seen.size > 10000)
          this.seen.delete(this.seen.values().next().value);
        for (const country of countries) {
          const previous = updates[country] ||
            state[country] || { count: 0, sum: 0 };
          updates[country] = {
            count: previous.count + 1,
            sum: previous.sum + sentiment,
          };
        }
      }
      return updates;
    });
  }

  renderMap() {
    if (!this.cell.filter) {
      return;
    }

    if (Object.keys(this.state).length === this.defaultLen) {
      return;
    }
    return (
      <ComposableMap
        role="img"
        aria-label="Average title sentiment by country; a text summary follows"
      >
        <Geographies geography={GEO_URL}>
          {({ geographies }) =>
            geographies.map((geo, index) => {
              const data = this.state[geo.properties.ISO_A3];
              const key =
                geo.rsmKey || geo.id || `${geo.properties.ISO_A3}-${index}`;
              const sentiment = data ? data.sum / data.count : null;
              const color =
                sentiment !== null ? colorScale(sentiment) : DEFAULT_COLOR;

              return (
                <Geography
                  key={key}
                  geography={geo}
                  fill={color}
                  tabIndex={-1}
                />
              );
            })
          }
        </Geographies>
      </ComposableMap>
    );
  }

  renderQueryBuilder() {
    const fields = {
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

  render() {
    return (
      <section className="widget" data-widget-id={this.props.cell.uuid}>
        {this.renderConfig()}
        {this.renderMap()}
        <details>
          <summary>Country sentiment summary</summary>
          {Object.entries(this.state)
            .filter(([, v]) => v && typeof v === "object" && v.count > 0)
            .map(([k, v]) => (
              <p key={k}>
                {k}: {(v.sum / v.count).toFixed(2)} ({v.count} samples)
              </p>
            ))}
        </details>
      </section>
    );
  }
}
