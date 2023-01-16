import React from 'react'
import Connectable from '../connectable'
import QueryBuilder from "../query"

const MAX_SIZE = 250
const DEFAULT_QUERY = {
    data_type: "article",
    query: {
        keyword: [],
        country: [],
        company: []
    },
    options: {
        preload: true
    }
}


export default class ArticleFeedView extends Connectable {
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
            err: "",
            articles: []
        }
    }

    onMessage(message) {
        let articles = this.state.articles.slice()

        for (let value of message.data) {
            articles.push(value)
        }

        while (articles.length > MAX_SIZE) {
            articles.shift()
        }

        this.setState({
            articles: articles
        })
    }

    renderArticleList() {
        return this.state.articles.map((article) => (
            <div key={article.url}>
                <a href={article.url} target="_blank" rel="noopener noreferrer">
                    {article.title}
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
            {this.renderArticleList()}
        </div>
    }
}
