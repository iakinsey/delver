import React from 'react';
import { request, hasUserInfo, routeTo } from '../../util'
import { input, btn } from '../../styles/input'
import { errorText } from '../../styles/err'
import { LINK_TEXT_ALT, BOX_BORDER, BOX_BG, LINK_TEXT } from '../../styles/color'


export default class DashboardList extends React.Component {
    constructor(props) {
        super(props)

        this.state = {
            addMenu: false,
            newDashName: "",
            newDashDesc: "",
            newDashConfig: "",
            dashboards: []
        }

        if (hasUserInfo()) {
            this.getDashboards()
        } else {
            routeTo('/login')
        }
    }

    async getDashboards() {
        const {data, err} = await request('dashboard/list')

        if (err) {
            return
        }

        this.setState({dashboards: data})
    }

    renderDashboards() {
        const dashBox = Object.assign({}, styles.dashBox, styles.dashBoxBody)

        if (!this.state.dashboards || !this.state.dashboards.length) {
            return
        }

        return (
            <div style={dashBox}>
                <div style={styles.dashes}>
                    {this.state.dashboards.filter((i) => i.name).map((dashboard) => (
                        <div key={dashboard.id}>
                            <div style={styles.dashName}>
                                <a style={styles.dashLink} href={`dashboard/${dashboard.id}`}>{dashboard.name}</a>
                            </div>
                            <div style={styles.dashDesc}>{dashboard.description}</div>
                        </div>
                    ))}
                </div>
            </div>
        )
    }

    renderAddError() {
        return <div style={errorText}>{this.state.createError}</div>
    }

    renderAddDashboard() {
        if (!this.state.addMenu) {
            return
        }

        const dashBox = Object.assign({}, styles.dashBox, styles.dashBoxBody)
        const dashInput = Object.assign({}, input, styles.newDashDesc)

        return (
            <div style={dashBox}>
                <input 
                 style={input}
                 placeholder="Name"
                 type="text"
                 value={this.state.newDashName}
                 onChange={(e) => this.setState({newDashName: e.target.value})} />
                <textarea
                 style={dashInput}
                 placeholder="Description"
                 type="text"
                 value={this.state.newDashDesc}
                 onChange={(e) => this.setState({newDashDesc: e.target.value})} />
                <textarea
                 style={dashInput}
                 placeholder="Config (Optional)"
                 type="text"
                 value={this.state.newDashConfig}
                 onChange={(e) => this.setState({newDashConfig: e.target.value})} />
                <button style={btn} type="button" onClick={() => this.addDash()}>Create</button>
            </div>
        )
    }

    async addDash() {
        const args = {
            name: this.state.newDashName,
            description: this.state.newDashDesc,
            value: JSON.parse(this.state.newDashConfig ? this.state.newDashConfig : '{}')
        }
        const {err} = await request('dashboard/save', args)

        if (err) {
            this.setState({createError: err})
            return
        }

        this.setState({
            newDashName: '',
            newDashDesc: '',
            addMenu: false
        })

        await this.getDashboards()
    }

    toggleAdd() {
        this.setState({
            addMenu: !this.state.addMenu
        })
    }

    render() {
        if (!hasUserInfo()) {
            return <div></div>
        }

        const dashLogo = Object.assign({}, styles.dashBox, styles.dashItemLogo)
        const dashAdd = Object.assign({}, styles.dashBox, styles.dashItemAdd)
 
        return (
            <div style={styles.dashContainer}>
                <div style={styles.topSpacing}></div>
                <div>
                    <div style={dashLogo}>Dashboards</div>
                    <div style={dashAdd} onClick={() => this.toggleAdd()}>+</div>
                </div>
                {this.renderAddDashboard()}
                {this.renderDashboards()}
            </div>
        )
    }
}


const styles = {
    topSpacing: {
        height: "25vh"
    },
    dashContainer: {
        margin: "0 auto",
        width: "1000px"
    },
    dashItemLogo: {
        display: "inline-block",
        width: "895px"
    },
    dashItemAdd: {
        display: "inline-block",
        width: "50px",
        marginLeft: "11px",
        cursor: 'pointer',
        color: LINK_TEXT_ALT,
        userSelect: 'none',
        fontWeight: 'bold'
    },
    dashBoxBody: {
        width: "960px"
    },
    newDashDesc: {
        height: "200px",
        resize: "vertical"
    },
    dashBox: {
        paddingTop: "4px",
        paddingBottom: "4px",
        border: "2px dashed",
        borderColor: BOX_BORDER,
        backgroundColor: BOX_BG,
        textAlign: "center",
        marginBottom: "1em"
    },
    dashes: {
        textAlign: "left",
        padding: "1em"
    },
    dashName: {
        fontSize: "2em"
    },
    dashDesc: {
        paddingBottom: "1em"
    },
    dashLink: {
        color: LINK_TEXT
    }
}
