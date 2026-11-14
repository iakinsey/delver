import React, { Component } from "react";
import { rebuildFilter } from "./rebuildFilter";
export { rebuildFilter } from "./rebuildFilter";

import {
  Utils as QbUtils,
  Query,
  Builder,
  BasicConfig,
} from "@react-awesome-query-builder/ui";
import "@react-awesome-query-builder/ui/css/compact_styles.css";
import { uuid4 } from "../util";

export default class QueryBuilder extends Component {
  constructor(props) {
    super(props);

    const InitialConfig = BasicConfig;
    const baseFilter = JSON.parse(props.filter);
    baseFilter.query ||= {};
    baseFilter.options ||= {};
    const accessibleFields = Object.fromEntries(
      Object.entries(props.fields).map(([key, field]) => [
        key,
        {
          ...field,
          fieldSettings: {
            ...field.fieldSettings,
            customProps: {
              "aria-label": field.label,
              ...(key === "key" ? { list: "supported-field-paths" } : {}),
              ...field.fieldSettings?.customProps,
            },
          },
        },
      ]),
    );
    const queryValue = this.getInitialQueryValue(baseFilter, props.fields);
    const updates = {};
    const config = {
      ...InitialConfig,
      settings: {
        ...InitialConfig.settings,
        maxNesting: 1,
        allowSelfNesting: false,
        showNot: false,
        addRuleLabel: "Add filter",
      },
      conjunctions: {
        AND: {
          label: "And",
          formatConj: (children, _conj, not) =>
            (not ? "NOT " : "") + "(" + children.join(" || ") + ")",
          reversedConj: "OR",
          mongoConj: "$and",
        },
      },
      fields: accessibleFields,
    };

    for (const [k, v] of Object.entries(props.fields)) {
      if (v.onUpdate) {
        updates[k] = v.onUpdate;
      }
    }

    this.updates = updates;
    this.baseFilter = baseFilter;
    this.fields = props.fields;
    this.onError = props.onError ? props.onError : (msg) => {};
    this.onUpdate = props.onUpdate;
    this.state = {
      tree: QbUtils.checkTree(QbUtils.loadTree(queryValue), config),
      config: config,
    };
  }

  getInitialQueryValue(filter, fields) {
    const queryValue = { id: QbUtils.uuid(), type: "group" };

    if (!fields) {
      return queryValue;
    }

    let children1 = [];

    for (let [key, value] of Object.entries(fields)) {
      if (!value.getter) {
        continue;
      }

      let val = "";

      try {
        val = value.getter(filter, key);
      } catch (e) {}

      if (val === undefined || val === "" || val === null) {
        continue;
      }

      let childVal = Array.isArray(val) ? val : [val];

      children1.push({
        id: uuid4(),
        type: "rule",
        properties: {
          field: key,
          operator: "equal",
          value: childVal,
          valueSrc: childVal.map(() => "value"),
          valueType: childVal.map(() => value.type),
        },
      });
    }

    if (children1) {
      queryValue.children1 = children1;
    }

    return queryValue;
  }

  render() {
    return (
      <div>
        <datalist id="supported-field-paths">
          <option value="features.sentiment.binary_naive_bayes_title" />
          <option value="http_code" />
          <option value="httpFetcher.message.in" />
        </datalist>
        <Query
          {...this.state.config}
          value={this.state.tree}
          onChange={this.onChange.bind(this)}
          renderBuilder={this.renderBuilder.bind(this)}
        />
      </div>
    );
  }

  renderBuilder(props) {
    return (
      <div className="query-builder-container" style={{ padding: "10px" }}>
        <div className="query-builder qb-lite">
          <Builder {...props} />
        </div>
      </div>
    );
  }

  onChange(immutableTree, config) {
    const tree = QbUtils.getTree(immutableTree);
    this.setState({ tree: immutableTree, config });
    const values = {};
    try {
      for (const criterion of tree.children1 || []) {
        const { field, value } = criterion.properties;
        if (!field || !value || value.some((v) => v === undefined))
          throw Error("Complete or remove the unfinished filter");
        if (field in values) throw Error(`Duplicate key: ${field}`);
        values[field] =
          this.fields[field].type === "datetime" ? value.join("|") : value[0];
      }
      this.onUpdate(rebuildFilter(this.baseFilter, this.fields, values));
      this.onError("");
    } catch (error) {
      this.onError(error.message);
    }
  }
}
