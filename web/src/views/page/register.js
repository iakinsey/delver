import React from 'react';
import { API_SERVER } from "../../config"
import { routeTo, setUserInfo, hasUserInfo } from '../../util'
import { btn, input } from '../../styles/input'
import { dashBox, errorText } from '../../styles/generic'

export default class Login extends React.Component {
    constructor(props) {
        super(props)

        this.state = {
            email: "",
            password: "",
            pconfirm: "",
            error: ""
        }
    }

    async register() {
        if (!this.passwordsMatch()) {
            this.setState({error: "Password mismatch"})
            return
        }
        const registerUrl = `${API_SERVER}/user/create`

        const response = await fetch(registerUrl, {
            method: "POST",
            body: JSON.stringify({
                email: this.state.email, 
                password: this.state.password
            })
        })
        const {success, data, error} = await response.json()

        if (!success) {
            this.setState({error: error})
            return
        }

        setUserInfo(this.state.email, data)

        routeTo("/dashboards")
    }

    passwordsMatch() {
        return this.state.password === this.state.pconfirm
    }

    renderError() { 
        return (
            <div style={errorText}>
                {this.state.error}
            </div>
        )
    }

    render() {
        if (hasUserInfo()) {
            routeTo('/dashboards')
            return <div></div>
        }

        return (
            <div>
                <div style={styles.topSpacing}></div>
                <div style={dashBox}>
                    <div style={styles.icon}>Registration</div>
                    <div>
                        <input 
                         style={input}
                         placeholder="email"
                         type="text"
                         value={this.state.email}
                         onChange={(e) => this.setState({"email": e.target.value})} />
                    </div>
                    <div>
                        <input 
                         style={input}
                         placeholder="password"
                         type="password" 
                         value={this.state.password}
                         onChange={(e) => this.setState({"password": e.target.value})} />
                    </div>
                    <div>
                        <input 
                         style={input}
                         placeholder="confirm password"
                         type="password" 
                         value={this.state.pconfirm}
                         onChange={(e) => this.setState({"pconfirm": e.target.value})} />
                    </div>
                    <div>
                        <button style={btn} type="button" onClick={() => this.register()}>Login</button>
                    </div>
                    {this.renderError()}
                </div>
            </div>
        )
    }
}

const styles = {
    icon: {
        fontSize: "2em",
        padding: "1em"
    },
    topSpacing: {
        height: "25vh"
    },
    header: {
        fontSize: "5em",
        marginTop: ".5em",
        marginBottom: ".5em"
    }
}
