import React from 'react'
import Connectable from '../connectable'
import QueryBuilder from "../query"
import { getDateQueryString } from "../../util"
import { LineChart, Line, CartesianGrid, XAxis, YAxis, ResponsiveContainer } from 'recharts';
import moment from 'moment'

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
                operators: ['equal'],
                getter: (filter, key) => filter.query[key],
                onUpdate: (filter, key, value) => filter.query[key] = value[0],
            },
            range: {
                label: 'Range',
                type: 'datetime',
                operators: ['between'],
                getter: (filter, key) => [
                    getDateQueryString(filter.query.start),
                    getDateQueryString(filter.query.end)
                ],
                onUpdate: (filter, key, value) => {
                    filter.query.start = new Date(value[0]).getTime() / 1000
                    filter.query.end = new Date(value[1]).getTime() / 1000
                },
            },
            preload: {
                label: 'Preload',
                type: 'boolean',
                operators: ['equal'],
                defaultValue: true,
                getter: (filter, key) => filter.options[key],
                onUpdate: (filter, key, value) => filter.options[key] = value[0]
            }
        }

        return  (
            <QueryBuilder
             filter={this.state.filter}
             fields={fields}
             onError={(msg) => this.setState({err: msg})}
             onUpdate={(d) => (
                this.setState({
                    filterInProgress: JSON.stringify(d),
                    err: undefined
                 })
            )} />
        )
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