import React from 'react'
import Connectable from '../connectable'
import { LineChart, Line, CartesianGrid, XAxis, YAxis, ResponsiveContainer } from 'recharts';
import moment from 'moment'

const TIMESTAMP_KEY = 'when'

const DEFAULT_QUERY = {
    data_type: "metric",
    query: {
        key: "fetcher.duration.millisecond"
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

    onMessage(message) {
        const newData = this.state.data.concat(message.data)
        console.log(newData)
        this.setState({ data: newData })
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