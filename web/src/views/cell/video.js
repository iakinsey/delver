import React from 'react'
import Connectable from '../connectable'
import ReactPlayer from "react-player"

const DEFAULT_QUERY = {                                         
    url: "https://www.youtube.com/watch?v=9Auq9mYxFEE"
}                                                               


export default class Map extends Connectable {
    constructor(props) {
        super(props)

        this.cell = props.cell
        const filter = props.cell.filter || DEFAULT_QUERY
        const filterJSON = JSON.stringify(filter)
 
        this.state = {
            filter: filterJSON,
            filterInProgress: filterJSON,
            url: filter.url,
            title: this.cell.title ? this.cell.title : '',
            configOn: this.cell.filter ? false : true,
            width: props.width,
            height: props.height
        }
    }

    async connect() {}

    renderVideo() {
        return (
            <ReactPlayer url={this.state.url} />
        )
    }

    render() {
        return <div>
            {this.renderConfig()}
            {this.renderVideo()}
        </div>
    }
}
