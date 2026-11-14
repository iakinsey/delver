import React from "react";
import { API_SERVER } from "../../config";
import { request, routeTo, hasUserInfo, setUserInfo } from "../../util";
import { btn, input } from "../../styles/input";
import { dashBox, dashBoxHeader, errorText } from "../../styles/generic";

export default class Login extends React.Component {
  constructor(props) {
    super(props);

    this.state = {
      email: "",
      password: "",
      error: "",
    };
  }

  async login() {
    const loginUrl = `${API_SERVER}/user/authenticate`;

    this.setState({ error: "", pending: true });
    const { success, data, error } = await request("user/authenticate", {
      email: this.state.email,
      password: this.state.password,
    });
    this.setState({ pending: false });

    if (!success) {
      this.setState({ error: error });
      return;
    }

    setUserInfo(this.state.email, data);

    routeTo("/dashboards");
  }

  renderError() {
    return (
      <div role="alert" style={errorText}>
        {this.state.error}
      </div>
    );
  }

  render() {
    if (hasUserInfo()) {
      routeTo("/dashboards");
      return <div></div>;
    }

    return (
      <div>
        <div style={styles.topSpacing}></div>
        <div style={dashBox}>
          <img style={dashBoxHeader} src="/loginkey.png" alt="login" />
          <div>
            <label style={{ display: "block" }}>
              <span className="sr-only">Email</span>
              <input
                style={input}
                aria-label="email"
                placeholder="email"
                type="text"
                value={this.state.email}
                onChange={(e) => this.setState({ email: e.target.value })}
              />
            </label>
          </div>
          <div>
            <label style={{ display: "block" }}>
              <span className="sr-only">Password</span>
              <input
                style={input}
                aria-label="password"
                placeholder="password"
                type="password"
                value={this.state.password}
                onChange={(e) => this.setState({ password: e.target.value })}
              />
            </label>
          </div>
          <div>
            <button
              style={btn}
              disabled={this.state.pending}
              type="button"
              onClick={() => this.login()}
            >
              Login
            </button>
          </div>
          <div style={styles.registerText}>
            <a href="/register">Register</a>
          </div>
          {this.renderError()}
        </div>
      </div>
    );
  }
}

const styles = {
  topSpacing: {
    height: "25vh",
  },
  loginHeader: {
    fontSize: "4em",
  },
  registerText: {
    padding: ".2em",
    fontSize: "1.8em",
  },
};
