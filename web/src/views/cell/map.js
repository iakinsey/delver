import React from 'react'
import Connectable from '../connectable'
import { scaleLinear } from "d3-scale";
import { getKey } from '../../util.js'
import {
    ComposableMap,
    Geographies,
    Geography
} from "react-simple-maps";


const DEFAULT_COLOR = "#323232"
const GEO_URL = "/world-110m.json"
const SENTIMENT_KEY = "binary_sentiment_naive_bayes_content"
const COUNTRY_KEY = "countries"
const TIMESTAMP_KEY = 'found'
const DEFAULT_QUERY = {
    fields: [SENTIMENT_KEY, TIMESTAMP_KEY, COUNTRY_KEY],
    key: SENTIMENT_KEY,
    title: "Sentiment",
    data_type: "article",
    options: {
        preload: true
    }
}

const colorScale = scaleLinear()
    .domain([0, 0.5, 1])
    .range(["#4d9508", "#ffeb00", "#cb0000"])



export default class Map extends Connectable {
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
            title: this.cell.title ? this.cell.title : '',
            configOn: this.cell.filter ? false : true,
            err: "",
            chart: chart,
        }

        this.defaultLen = Object.keys(this.state).length
    }

    onMessage(message) {
        let changed = false;
        let newState = {}

        for (let entity of message.data) {
            if (!entity[COUNTRY_KEY] || !entity[SENTIMENT_KEY]) {
                continue
            }

            changed = true

            for (let country of entity[COUNTRY_KEY]) {
                var entry

                if (!this.state[country]) {
                    entry = this.state[country] = { count: 0, sum: 0 }
                } else {
                    entry = Object.assign({}, this.state[country])
                }

                entry.count += 1
                entry.sum += entity[SENTIMENT_KEY]

                newState[country] = entry
            }
        }

        if (changed) {
            this.setState(newState)
        }
    }

    renderMap() {
        if (!this.cell.filter) {
            return
        }

        if (Object.keys(this.state).length === this.defaultLen) {
            return
        }
        return (
            <ComposableMap>
                <Geographies geography={GEO_URL}>
                    {({ geographies }) =>
                        geographies.map((geo) => {
                            const data = this.state[geo.properties.ISO_A3]
                            const key = getKey(geo.properties.ISO_A3, data ? data : {})
                            const sentiment = data ? data.sum / data.count : null
                            const color = sentiment ? colorScale(sentiment) : DEFAULT_COLOR

                            return <Geography key={key} geography={geo} fill={color} />
                        })
                    }
                </Geographies>
            </ComposableMap>
        )
    }

    render() {
        return <div>
            {this.renderConfig()}
            {this.renderMap()}
        </div>
    }
}
