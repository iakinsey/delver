import React from 'react'
import Connectable from '../connectable'
import QueryBuilder from "../query"
import { LineChart, Line, CartesianGrid, XAxis, YAxis, ResponsiveContainer } from 'recharts';
import moment from 'moment'

const TIMESTAMP_KEY = 'when'

const DEFAULT_QUERY = {
    data_type: "metric",
    query: {
        key: "httpFetcher.message.in"
    },
    options: {
        preload: true
    }
}

const formatXAxis = tickItem => {
    return moment.unix(tickItem).format('D MMM')
}

export default class MetricView extends Connectable {
    constructor(props) {
        super(props)

        this.cell = props.cell
        const filter = props.cell.filter || DEFAULT_QUERY
        const filterJSON = JSON.stringify(filter)
        const chart = { data: [] }

        if (filter.title) {
            chart.label = filter.title
        }
        this.state = {
            filter: filterJSON,
            filterInProgress: filterJSON,
            configOn: this.cell.filter ? false : true,
            title: this.cell.title ? this.cell.title : '',
            err: "",
            chart: chart,
            data: []
        }
    }

    onMessage(message, reconnect) {
        const newData = this.state.data.concat(message.data)
        this.setState({ data: newData })
    }

    renderQueryBuilder() {
        const fields = {
            key: {
                label: 'Key',
                type: 'text',
                operators: ['equal']
            },
            range: {
                label: 'Range',
                type: 'datetime',
                operators: ['between']
            },
            preload: {
                label: 'Preload',
                type: 'boolean',
                operators: ['equal'],
                defaultValue: true
            }
      }

        return <QueryBuilder onChange={this.onChange.bind(this)} fields={fields} />
    }

    onChange(data) {
        let seen = {}
        let filter = {
            data_type: "metric",
            query: {},
            options: {}
        }

        for (const criterion of data.children1 ? data.children1 : []) {
            var properties = criterion.properties
            var key = properties.field
            var value =  properties.value

            if (key === null) {
                continue
            }

            if (seen[key] || false) {
                this.setState({err: `Duplicate key: ${key}`})
                return
            }

            if ((key === "key") && value[0] !== undefined) {
                filter.query[key] = value[0]
            } if ((key === "preload") && value[0] !== undefined) {
                filter.options[key] = value[0]
            } else if (key === "range" && value[0] !== undefined && value[1] !== undefined) {
                filter.query.start = new Date(value[0]).getTime() / 1000
                filter.query.end = new Date(value[1]).getTime() / 1000
            }

            seen[key] = true
        }

        this.setState({filterInProgress: JSON.stringify(filter), err: undefined})
    }
    renderMetric() {
        if (!this.cell.filter) {
            return
        }

        return (
            <ResponsiveContainer width="99%" height="92%">
                <LineChart data={this.state.data} margin={{ top: 5, right: 5, bottom: 5 }}>
                    <Line type="monotone" dataKey="value" stroke="#8884d8"  dot={false} />
                    <CartesianGrid stroke="#ccc" strokeDasharray="5 5" />
                    <XAxis dataKey="when" tickFormatter={formatXAxis} />
                    <YAxis />
                </LineChart>
            </ResponsiveContainer>
        )
    }

    render() {
        return <span>
            {this.renderConfig()}
            {this.renderMetric()}
        </span>
    }
}