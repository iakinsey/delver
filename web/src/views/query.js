import React, {Component} from 'react';

import { Utils as QbUtils, Query, Builder, BasicConfig } from '@react-awesome-query-builder/ui';
import '@react-awesome-query-builder/ui/css/compact_styles.css';
import { uuid4 } from '../util'

export default class QueryBuilder extends Component {
  constructor(props) {
    super(props)

    const InitialConfig = BasicConfig;
    const baseFilter = JSON.parse(props.filter)
    const queryValue = this.getInitialQueryValue(
      baseFilter,
      props.fields
    )
    const updates = {}
    const config = {
      ...InitialConfig,
      settings: {
        ...InitialConfig.settings,
        maxNesting: 1,
        allowSelfNesting: false,
        showNot: false,
        addRuleLabel: "+"
      },
      conjunctions: {
        AND: {
          label: 'And',
          formatConj: (children, _conj, not) => ( (not ? 'NOT ' : '') + '(' + children.join(' || ') + ')' ),
          reversedConj: 'OR',
          mongoConj: '$and',
        },
      },
      fields: props.fields
    };

    for (const [k, v] of Object.entries(props.fields)) {
      if (v.onUpdate) {
        updates[k] = v.onUpdate
      }
    }

    this.updates = updates
    this.baseFilter = baseFilter
    this.fields = props.fields
    this.onError = props.onError ? props.onError : (msg) => {}
    this.onUpdate = props.onUpdate
    this.state = {
      tree: QbUtils.checkTree(QbUtils.loadTree(queryValue), config),
      config: config
    }
  }

  getInitialQueryValue(filter, fields) {
    const queryValue = {"id": QbUtils.uuid(), "type": "group"};

    if (!fields) {
      return queryValue
    }

    let children1 = []

    for (let [key, value] of Object.entries(fields)) {
      if (!value.getter) {
        continue
      }

      var val;

      try {
        val = value.getter(filter, key)
      } catch (e) {}

      if (val === undefined || val === "" || val === null || val === NaN) {
        continue
      }

      let childVal = Array.isArray(val) ? val : [val]

      children1.push({
        id: uuid4(),
        type: 'rule',
        properties: {
          field: key,
          operator: "equal",
          value: childVal,
          valueSrc: childVal.map(() => "value"),
          valueType: childVal.map(() => value.type)
        }
      })
    }

    if (children1) {
      queryValue.children1 = children1
    }

    return queryValue
  }
 
  render() {
    return <div>
      <Query
        {...this.state.config} 
        value={this.state.tree}
        onChange={this.onChange.bind(this)}
        renderBuilder={this.renderBuilder.bind(this)}
      />
    </div>
  }

  renderBuilder(props) {
    return <div className="query-builder-container" style={{padding: '10px'}}>
      <div className="query-builder qb-lite">
        <Builder {...props} />
      </div>
    </div>
  }

  onChange(immutableTree, config) {
    const data = QbUtils.getTree(immutableTree);
    let seen = {}
    let newFilter = JSON.parse(JSON.stringify(this.baseFilter))

    this.setState({tree: immutableTree, config: config});

    for (const criterion of data.children1 ? data.children1 : []) {
      let properties = criterion.properties
      let key = properties.field
      let value = properties.value

      if (!key) {
        continue
      }

      if (seen[key] || false) {
        this.onError(`Duplicate key: ${key}`)
        return
      }

      seen[key] = true

      for (const v of value) {
        if (v === undefined) {
          continue
        }
      }

      if (this.updates[key]) {
        this.updates[key](newFilter, key, value)
      }
    }

    this.onUpdate(newFilter)
  }
}