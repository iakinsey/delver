import React from 'react';
import { API_SERVER } from "../../config"
import { routeTo, hasUserInfo, setUserInfo } from '../../util'
import { btn, input } from '../../styles/input'
import { dashBox, dashBoxHeader, errorText } from '../../styles/generic'


export default class Login extends React.Component {
    constructor(props) {
        super(props)

        this.state = {
            email: "",
            password: "",
            error: ""
        }
    }

    async login() {
        const loginUrl = `${API_SERVER}/user/authenticate`

        const response = await fetch(loginUrl, {
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

        setUserInfo(
            this.state.email,
            data
        )

        routeTo("/dashboards")
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
                    <img style={dashBoxHeader} src="/loginkey.png" alt="login"/>
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
                        <button style={btn} type="button" onClick={() => this.login()}>Login</button>
                    </div>
                    <div style={styles.registerText}><a href="/register">Register</a></div>
                    {this.renderError()}
                </div>
            </div>
        )
    }
}

const styles = {
    topSpacing: {
        height: "25vh"
    },
    loginHeader: {
        fontSize: "4em"
    },
    registerText: {
        padding: ".2em",
        fontSize: "1.8em"
    }
}
