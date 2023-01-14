import React, {Component} from 'react';

import { Utils as QbUtils, Query, Builder, BasicConfig } from '@react-awesome-query-builder/ui';
import '@react-awesome-query-builder/ui/css/compact_styles.css';

export default class QueryBuilder extends Component {
  constructor(props) {
    super(props)

    const InitialConfig = BasicConfig;
    const queryValue = {"id": QbUtils.uuid(), "type": "group"};
   
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
      fields: this.props.fields
    };

    this.updateParent = props.onChange;
    this.state = {
      tree: QbUtils.checkTree(QbUtils.loadTree(queryValue), config),
      config: config
    }
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
    this.setState({tree: immutableTree, config: config});

    const jsonTree = QbUtils.getTree(immutableTree);

    this.updateParent(jsonTree)
  }
}