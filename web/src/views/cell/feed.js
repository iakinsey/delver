import React from 'react'
import Connectable from '../connectable'

const MAX_SIZE = 250
const DEFAULT_QUERY = {
    data_type: "article",
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

    render() {
        return <div>
            {this.renderConfig()}
            {this.renderArticleList()}
        </div>
    }
}
