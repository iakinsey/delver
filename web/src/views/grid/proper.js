import GridLayout from 'react-grid-layout';
import React from 'react';
import ArticleFeedView from "../cell/feed"
import ChartView from "../cell/chart"
import EntityFeedView from "../cell/entity"
import MapView from "../cell/map"
import { getCells, getWindowSize } from '../../event/selectors'
import { updateCell } from '../../event/actions'
import { connect } from 'react-redux'
import md5 from 'md5'
import store from "../../event/store"
import MetricView from '../cell/metric';


const getCellKey = (cell) => {
    const contents = Object.assign({}, cell, {attributes: ''})
    return md5(JSON.stringify(contents))
}

const renderCellContents = (cell, dimensions) => {
    const key = getCellKey(cell)

    switch (cell.type) {
        case "textFeed": 
            return <ArticleFeedView key={key} cell={cell} />
        case "chart":
            return <ChartView key={key} cell={cell} />
        case "map":
            return <MapView key={key} cell={cell} />
        case "entityFeed":
            return <EntityFeedView key={key} cell={cell} />
        case "metric":
            return <MetricView key={key} cell={cell} />
        default:
            return
    }
}

const getUuid = (e) => {
    const t = e.target;
    const p = ((t || {}).parentElement || {}).parentElement

    if (p) {
        return p.getAttribute('uuid')
    }
}

const GridProper = ({ cells, width }) => {
    const handleResize = (l, o, n, p, e, m) => {
        const uuid = getUuid(e)
 
        if (!uuid) {
            return 
        }

        const cell = cells[uuid]

        store.dispatch(
            updateCell({
                uuid: cell.uuid,
                attributes: {x: n.x, y: n.y, w: n.w, h: n.h}
            })
        )
    }

    return (
        <GridLayout className="layout" cols={72} rowHeight={90} width={width}
         onResize={handleResize} onDragStop={handleResize} draggableHandle=".cell-header">
            {Object.values(cells).map((cell) => (
                <div 
                 key={cell.uuid}
                 uuid={cell.uuid}
                 style={styles.cell}
                 data-grid={cell.attributes}>
                    {renderCellContents(cell)}
                </div>
            ))}
        </GridLayout>
    )
}


const mapStateToProps = state => {
    const cells = Object.assign({}, getCells(state))
    const windowSize = getWindowSize(state)

    return {
        cells: cells,
        width: windowSize.width
    }
}


export default connect(
    mapStateToProps,
    undefined,
    undefined,
    {areStatesEqual: () => false}
)(GridProper)


const styles = {
    cell: {
        border: "2px dashed",
        borderColor: "#0b0b0b",
        backgroundColor: "#1c1c1c",
        fontWeight: "bolder",
        overflowWrap: "break-word",
        overflow: "hidden"
    }
}
