import React from 'react';
import { routeTo } from '../util'


function handleChange(e) {
    routeTo(e.target.value)
}

export default function Dropdown() {
    return (
        <select aria-label="Settings" onChange={handleChange} value="" name="Settings" style={dropdown}>
            <option style={{display: "none"}}></option>
            <option value="/help">Help</option>
            <option value="/crawler">Crawler</option><option value="/dashboards">Dashboards</option>
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
