import React from 'react'
import Connectable from '../connectable'
import { LineChart, Line, CartesianGrid, XAxis, YAxis, ResponsiveContainer } from 'recharts';
import moment from 'moment'

const TIMESTAMP_KEY = 'found'
const DEFAULT_QUERY = {
    fields: ["binary_sentiment_naive_bayes_aggregate", "found"],
    key: "binary_sentiment_naive_bayes_aggregate",
    title: "Sentiment",
    data_type: "article",
    agg: {
        time_field: "found",
        agg_field: "binary_sentiment_naive_bayes_aggregate",
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
            <ResponsiveContainer width="99%" height="92">
                <LineChart data={this.state.data} margin={{ top: 5, right: 5, bottom: 5 }}>
                    <Line type="monotone" dataKey={this.cell.filter.key} stroke="#8884d8" strokeWidth={3} dot={false} />
                    <CartesianGrid stroke="#ccc" strokeDasharray="5 5" />
                    <XAxis dataKey={TIMESTAMP_KEY} tickFormatter={formatXAxis} />
                    <YAxis />
                </LineChart>
            </ResponsiveContainer>
        )
    }

    render() {
        return <span>
            {this.renderConfig()}
            {this.renderChart()}
        </span>
    }
}
