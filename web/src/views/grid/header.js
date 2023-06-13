import React from 'react';
import { setCell } from '../../event/actions'
import { getCells, getDashboard } from '../../event/selectors'
import { uuid4, request } from '../../util'
import store from "../../event/store"
import {
    HEADER_BG, HEADER_BORDER, BOX_BORDER, BOX_BG, BUTTON_TEXT_ALT, ERROR_TEXT,
    LOGO
} from '../../styles/color'

const CELL_TYPES = {
    entityFeed: "Entity Feed",
    chart: "Chart",
    map: "Map",
    metric: "Metric"
}

const CELL_ATTR_DEFAULT = {x: 0, y: 0, w: 32, h: 8}

export default class GridHeader extends React.Component {
    constructor(props) {
        super(props)

        this.state = {
            configOn: false,
            error: ""
        }
    }

    renderConfig() {
        if (!this.state.configOn) {
            return
        }

        return (
            <div style={styles.config}>
                {
                    Object.keys(CELL_TYPES).map(k => (
                        <div 
                         key={k}
                         onClick={() => this.addCell(k)}
                         style={styles.configAddOption}>
                            {CELL_TYPES[k]}
                        </div>
                    ))
                }
            </div>
        )
    }

    async save() {
        const state = store.getState()
        const dash = getDashboard(state)

        const params = {
            id: dash.id,
            name: dash.name,
            description: dash.description,
            value: {cells: Object.values(getCells(state))}
        }

        const {error} = await request('dashboard/save', params)

        if (error) {
            this.setState({error: error})
        }
    }

    renderHeader() {
        return (
            <div style={styles.addCellContainer}>
                <span style={styles.addCell}
                onClick={() => this.setState({configOn: !this.state.configOn})}>
                    <img src="/add.png" style={styles.saveIcon} alt="add" />
                </span>
                <span style={styles.addCell}
                onClick={() => this.save()}>
                    <img src="/save.png" style={styles.saveIcon} alt="save" />
                </span>
            </div>
        )
    }

    addCell(type) {
        store.dispatch(
            setCell({
                uuid: uuid4(),
                type: type,
                attributes: CELL_ATTR_DEFAULT
            })
        )
    }

    renderError() {
        if (!this.state.error) {
            return
        }

        return (
            <div style={styles.error}>{this.state.error}</div>
        )
    }

    render() {
        return (
            <span>
                {this.renderHeader()}
                {this.renderConfig()}
                {this.renderError()}
            </span>
        )
    }
}


const styles = {
    config: {
        backgroundColor: HEADER_BG,
        margin: "4px",
        padding: "4px",
        border: "2px dashed",
        borderColor: HEADER_BORDER
    },
    configAddOption: {
        border: "2px dashed",
        borderColor: BOX_BORDER,
        backgroundColor: BOX_BG,
        fontWeight: "bolder",
        width: "100px",
        height: "60px",
        paddingTop: "40px",
        textAlign: "center",
        verticalAlign: "middle",
        userSelect: 'none',
        display: "inline-block",
        margin: "4px",
        cursor: "pointer"
    },
    logo: {
        position: 'absolute',
        fontSize: '1.8em',
        top: 12,
        left: 12,
        color: LOGO
    },
    addContainer: {
        position: 'absolute',
        top: 12,
        right: 50
    },
    addCellContainer: {
        position: 'absolute',
        top: 20,
        right: 68
    },
    addCell: {
        cursor: 'pointer',
        padding: "4px",
        color: BUTTON_TEXT_ALT,
        fontSize: '2.3em',
        textAlign: 'right',
        userSelect: 'none'
    },
    saveIcon: {
        width: '40px'
    },
    error: {
        textAlign: 'center',
        color: ERROR_TEXT,
        fontSize: "1.5em"
    }
}
