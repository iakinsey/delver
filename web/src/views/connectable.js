import React from 'react';
import RPC from '../event/rpc';
import { validateJSON, uuid4 } from '../util';
import store from "../event/store"
import { updateCell, deleteCell } from '../event/actions'
import { ERROR_TEXT, ICON } from '../styles/color'
import { btn } from '../styles/input'
import ClickableText from './clickableText'


const rpc = new RPC()


export default class Connectable extends React.Component {
    constructor(props) {
        super(props)

        this.uuid = uuid4()

        if (props.cell.filter) {
            setTimeout(() => this.connect(), 0)
        }
    }

    async connect() {
        rpc.addFilter(
            this.uuid,
            this.state.filter,
            this.onMessage.bind(this)
        )
    }

    onMessage(message) {
        console.log("Implement Connectable", message)
    }

    updateConfig() {
        console.log(this.state.filterInProgress)
        const err = validateJSON(this.state.filterInProgress)

        if (err) {
            this.setState({err: err})
            return
        }

        rpc.removeFilter(this.uuid)
        this.setState({configOn: false})
        store.dispatch(
            updateCell({
                uuid: this.cell.uuid,
                filter: JSON.parse(this.state.filterInProgress)
            })
        )
    }

    renderError() {
        if (!this.state.err) {
            return
        }

        return (
            <div style={styles.err}>
                {this.state.err}
            </div>
        )
    }

    closeCell(e) {
        rpc.removeFilter(this.uuid)
        // Hack to get the uuid
        store.dispatch(deleteCell(this.cell.uuid))
    }

    updateTitle(text) {
        store.dispatch(updateCell({
            uuid: this.cell.uuid,
            title: text
        }))
    }

    renderQueryBuilder() {
        return (
            <div>
                <textarea
                    onChange={(e) => this.setState({
                        filterInProgress: e.target.value
                    })}
                    style={styles.configBox}
                    value={this.state.filterInProgress} />
            </div>
        )
    }

    renderConfig() {
        if (this.state.configOn) {
            return (
                <div>
                   <div>
                        {this.renderQueryBuilder()}
                        {this.renderError()}
                        <button 
                         style={Object.assign({}, btn, styles.saveButton)}
                         onClick={() => this.updateConfig()}>
                            Save
                        </button>
                    </div>
                </div>
            )
        } else {
            return (
                <div style={styles.configGearBox} className="cell-header">
                    <div style={{float: 'left'}}>
                        <ClickableText onChange={(t) => this.updateTitle(t)} value={this.state.title} />
                    </div>
                    <div style={{float: 'right'}}>
                        <img
                         style={styles.configGear}
                         alt="config"
                         src="/gear.png"
                         onClick={() => this.setState({configOn: true})} />
                        <img 
                         style={styles.configGear}
                         alt="close"
                         src="/closeCell.png"
                         onClick={(e) => this.closeCell(e)} />
                    </div>
                </div>
            )
        }
    }
}

const styles = {
    configGearBox: {
        display: 'block',
        height: "25px",
        padding: '4px',
        backgroundColor: '#2a2a2a'
    },
    configGear: {
        cursor: 'pointer',
        color: ICON,
        padding: 0,
        userSelect: 'none'
    },
    err: {
        color: ERROR_TEXT
    },
    saveButton: {
        width: "100%",
        margin: "0",
        marginBottom: "0"
    },
    configBox: {
        width: "100%",
        height: "100%"
    }
}
