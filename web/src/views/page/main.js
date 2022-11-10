import React from 'react';
import { Link } from "react-router-dom";

export default class MainPage extends React.Component {
    constructor(props) {
        super(props)

        this.state = {
        }
    }

    render() {
        return (
            <span>
                <Link to="/grid">GRID</Link><br />
                <Link to="/login">LOGIN</Link><br />
                <Link to="/logout">LOGOUT</Link><br />
                <Link to="/register">REGISTER</Link><br />
                <Link to="/dashboards">DASHBOARDS</Link><br />
                <Link to="/changePassword">CHANGE PASSWORD</Link><br />
                <Link to="/help">HELP</Link><br />
            </span>
        )
    }
}
