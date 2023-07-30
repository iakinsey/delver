import React from 'react'
import Connectable from '../connectable'
import QueryBuilder from "../query"
import { getNestedAttribute } from '../../util'

const MAX_SIZE = 250
const DEFAULT_QUERY = {
    data_type: "composite",
    key: "uri",
    title_key: "features.title",
    query: {
        url: [],
        domain: [],
        http_code: [],
        title: [],
        language: []
    },
    options: {
        preload: true
    }
}


export default class EntityFeedView extends Connectable {
    constructor(props) {
        super(props)

        this.cell = props.cell
        this.conn = null;
        const filter = props.cell.filter || DEFAULT_QUERY
        const filterJSON = JSON.stringify(filter)

        this.state = {
            filter: filterJSON,
            filterInProgress: filterJSON,
            title: this.cell.title ? this.cell.title : '',
            configOn: this.cell.filter ? false : true,
            key: filter.key,
            titleKey: filter.title_key ? filter.title_key : filter.key,
            err: "",
            entities: []
        }
    }

    onMessage(message) {
        let entities = this.state.entities.slice()

        for (let value of message.data) {
            entities.push(value)
        }

        while (entities.length > MAX_SIZE) {
            entities.shift()
        }

        this.setState({
            entities: entities
        })
    }

    renderEntityList() {
        return this.state.entities.map((entity) => (
            <div key={entity[this.state.key]}>
                <a href={getNestedAttribute(entity, this.state.key)} target="_blank" rel="noopener noreferrer">
                    {this.getEntityValOrDefault(entity)}
                </a>
            </div>
        ))
    }

    getEntityValOrDefault(entity) {
        const val = getNestedAttribute(entity, this.state.titleKey)

        if (val) {
            return val
        }

        return getNestedAttribute(entity, this.state.key)
    }

    renderQueryBuilder() {
        const fields = {
            preload: {
                label: 'Preload',
                type: 'boolean',
                operators: ['equal'],
                defaultValue: true,
                getter: (filter, key) => filter.options[key],
                onUpdate: (filter, key, value) => filter.options[key] = value[0]
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
            url: {
                label: "URL",
                type: 'text',
                operators: ['equal'],
                getter: (filter, key) => filter.query.url.join(" "),
                onUpdate: (filter, key, value) => filter.query.url = value[0].split(" ")
            },
            domain: {
                label: "Domain",
                type: 'text',
                operators: ['equal'],
                getter: (filter, key) => filter.query.domain.join(" "),
                onUpdate: (filter, key, value) => filter.query.domain = value[0].split(" ")
            },
            http_code: {
                label: "HTTP Code",
                type: 'text',
                operators: ['equal'],
                getter: (filter, key) => filter.query.http_code.join(" "),
                onUpdate: (filter, key, value) => filter.query.http_code = value[0].split(" ").map((v) => Number(v))
            },
            title: {
                label: "Title",
                type: 'text',
                operators: ['equal'],
                getter: (filter, key) => filter.query.title.join(" "),
                onUpdate: (filter, key, value) => filter.query.title = value[0].split(" ")
            },
            language: {
                label: "Language",
                type: 'text',
                operators: ['equal'],
                getter: (filter, key) => filter.query.language.join(" "),
                onUpdate: (filter, key, value) => filter.query.language= value[0].split(" ")
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
        return <div>
            {this.renderConfig()}
            {this.renderEntityList()}
        </div>
    }
}
