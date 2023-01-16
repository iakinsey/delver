import React from 'react'
import Connectable from '../connectable'
import QueryBuilder from "../query"

const MAX_SIZE = 250
const DEFAULT_QUERY = {
    data_type: "page",
    key: "url",
    title_key: "title",
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
                <a href={entity[this.state.key]} target="_blank" rel="noopener noreferrer">
                    {entity[this.state.titleKey] ? entity[this.state.titleKey] : entity[this.state.key]}
                </a>
            </div>
        ))
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
