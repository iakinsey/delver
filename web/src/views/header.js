import Dropdown from './dropdown'
import React from 'react';


export default function Header() {
    return (
        <div style={styles.container}>
            <a href="/"><span style={styles.logo}>Delver</span></a>
            <span style={styles.dropdown}>
                <Dropdown />
            </span>
        </div>
    )
}


const styles = {
    logo: {
        position: 'absolute',
        fontSize: '3em',
        top: 12,
        left: 12,
        color: "#8282a7"
    },
    container: {
        textAlign: 'right',
        height: "60px",
        display: "block"
    },
    dropdown: {
        position: 'absolute',
        top: 12,
        right: 12,
        textAlign: 'right',
    },
}
