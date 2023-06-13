import React from 'react'
import Connectable from '../connectable'
import { LineChart, Line, CartesianGrid, XAxis, YAxis, ResponsiveContainer } from 'recharts';
import QueryBuilder from "../query"
import moment from 'moment'

const TIMESTAMP_KEY = 'timestamp'
const DEFAULT_QUERY = {
    fields: ["features.sentiment.binary_sentiment_naive_bayes_aggregate", "timestamp"],
    key: "features.sentiment.binary_sentiment_naive_bayes_aggregate",
    title: "Sentiment",
    data_type: "composite",
    query: {
        keyword: [],
        country: [],
        company: []
    },
    agg: {
        time_field: "timestamp",
        agg_field: "features.sentiment.binary_sentiment_naive_bayes_aggregate",
        time_window_seconds: 1800,
        agg_name: "avg"
    },
    options: {
        preload: true
    }
}

const formatXAxis = tickItem => {
    return moment.unix(tickItem).format('D MMM')
}

export default class _Chart extends Connectable {
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

    onMessage(message) {
        const newData = this.state.data.concat(message.data)
        this.setState({ data: newData })
    }


    renderChart() {
        if (!this.cell.filter) {
            return
        }

        return (
            <ResponsiveContainer width="99%" height="92%">
                <LineChart data={this.state.data} margin={{ top: 5, right: 5, bottom: 5 }}>
                    <Line type="monotone" dataKey={this.cell.filter.key} stroke="#8884d8" strokeWidth={3} dot={false} />
                    <CartesianGrid stroke="#ccc" strokeDasharray="5 5" />
                    <XAxis dataKey={TIMESTAMP_KEY} tickFormatter={formatXAxis} />
                    <YAxis />
                </LineChart>
            </ResponsiveContainer>
        )
    }

    renderQueryBuilder() {
        const fields = {
            key: {
                label: 'Key',
                type: 'text',
                operators: ['equal'],
                getter: (filter, key) => filter.query[key],
                onUpdate: (filter, key, value) => filter.query[key] = value[0]
            },
            preload: {
                label: 'Preload',
                type: 'boolean',
                operators: ['equal'],
                defaultValue: true,
                getter: (filter, key) => filter.options[key],
                onUpdate: (filter, key, value) => filter.options[key] = value[0]
            },
            agg_field: {
                label: "Aggregate Field",
                type: 'text',
                operators: ['equal'],
                getter: (filter, key) => filter.agg.agg_field,
                onUpdate: (filter, key, value) => {
                    filter.agg.agg_field = value[0]
                    filter.fields.push(value[0])
                }
            },
            time_field: {
                label: "Time Field",
                type: 'text',
                operators: ['equal'],
                defaultValue: 'found',
                getter: (filter, key) => filter.agg.time_field,
                onUpdate: (filter, key, value) => {
                    filter.agg.time_field = value[0]
                    filter.fields.push(value[0])
                }
            },
            keyword: {
                label: "Keywords",
                type: 'text',
                operators: ['equal'],
                getter: (filter, key) => filter.query.keyword.join(" "),
                onUpdate: (filter, key, value) => filter.query.keyword = value[0].split(" ")
            },
            country: {
                label: "Countries (ISO 3166-1 alpha-2)",
                type: 'text',
                operators: ['equal'],
                getter: (filter, key) => filter.query.country.join(" "),
                onUpdate: (filter, key, value) => filter.query.country = value[0].split(" ")
            },
            company: {
                label: "Company (Exchange:Ticker)",
                type: 'text',
                operators: ['equal'],
                getter: (filter, key) => filter.query.company.join(" "),
                onUpdate: (filter, key, value) => filter.query.company = value[0].split(" ")
            },
            time_window: {
                label: "Aggregate Time Window (Seconds)",
                type: "number",
                operators: ['equal'],
                defaultValue: 1800,
                getter: (filter, key) => filter.agg.time_window_seconds,
                onUpdate: (filter, key, value) => filter.agg.time_window_seconds = value[0]
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

    render() {
        return <span>
            {this.renderConfig()}
            {this.renderChart()}
        </span>
    }
}
