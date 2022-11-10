import React from 'react';
import { ICON } from '../styles/color'

export default class ClickableText extends React.Component {
    constructor(props) {
        super(props)

        this.onChange = props.onChange

        this.state = {
            value: props.value ? props.value: '',
            toggled: props.value ? false : true
        }
    }

    onKeyDown(e) {
        if (e.key === 'Enter' && this.onChange) {
            this.onChange(this.state.value)
            this.setState({toggled: false})
        }
    }
    render() {
        if (this.state.toggled) {
            return (
                <input
                 type="text" name="name" value={this.state.value}
                 onChange={(e) => this.setState({value: e.target.value})} 
                 onKeyDown={(e) => this.onKeyDown(e)}
                 placeholder="title" style={styles.input} />
            )
        } else {
            return (
                <div 
                 style={styles.text}
                 onClick={() => this.setState({toggled: true})}>
                    {this.state.value}
                </div>
            )
        }
    }
}

const styles = {
    text: {
        cursor: "pointer",
        fontSize: "1.6em",
        color: ICON
    },
    input: {
        fontSize: ".7em"
    }
}
