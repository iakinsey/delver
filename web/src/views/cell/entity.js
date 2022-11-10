import React from 'react'
import Connectable from '../connectable'

const MAX_SIZE = 250
const DEFAULT_QUERY = {
    data_type: "page",
    key: "url",
    title_key: "title",
    query: {},
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

    render() {
        return <div>
            {this.renderConfig()}
            {this.renderEntityList()}
        </div>
    }
}
