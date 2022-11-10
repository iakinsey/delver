import React from 'react';
import { routeTo, hasUserInfo } from '../util'


function handleChange(e) {
    routeTo(e.target.value)
}

export default function Dropdown() {
    const loggedIn = hasUserInfo()

    const loggedInValues = [
        <option key="changePw" value="/changePassword">Change Password</option>,
        <option key="dash" value="/dashboards">Dashboards</option>,
        <option key="logout" value="/logout">Logout</option>
    ]

    const loggedOutValues = [
        <option key="login" value="/login">Login</option>,
        <option key="register" value="/register">Register</option>
    ]
 
    return (
        <select onChange={handleChange} value="" name="Settings" style={dropdown}>
            <option style={{display: "none"}}></option>
            <option value="/help">Help</option>
            {loggedIn 
                ? loggedInValues
                : loggedOutValues}
        </select>
    ) 
}


export const dropdown = {
    border: "0",
    background: "transparent",
    margin: ".3em",
    fontSize: "1em",
    backgroundImage: "url(/gearToggle.png)",
    backgroundPosition: "center",
    backgroundSize: "contain",
    backgroundRepeat: "no-repeat",
    userSelect: "none",
    width: "42px",
    height: "46px"
}
