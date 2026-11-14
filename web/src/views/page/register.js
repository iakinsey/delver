import React from "react";
import { API_SERVER } from "../../config";
import { request, routeTo, setUserInfo, hasUserInfo } from "../../util";
import { btn, input } from "../../styles/input";
import { dashBox, errorText } from "../../styles/generic";

export default class Login extends React.Component {
  constructor(props) {
    super(props);

    this.state = {
      email: "",
      password: "",
      pconfirm: "",
      error: "",
    };
  }

  async register() {
    if (!this.passwordsMatch()) {
      this.setState({ error: "Password mismatch" });
      return;
    }
    const registerUrl = `${API_SERVER}/user/create`;

    this.setState({ error: "", pending: true });
    const { success, data, error } = await request("user/create", {
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

  passwordsMatch() {
    return this.state.password === this.state.pconfirm;
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
          <div style={styles.icon}>Registration</div>
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
            <label style={{ display: "block" }}>
              <span className="sr-only">Confirm password</span>
              <input
                style={input}
                aria-label="confirm password"
                placeholder="confirm password"
                type="password"
                value={this.state.pconfirm}
                onChange={(e) => this.setState({ pconfirm: e.target.value })}
              />
            </label>
          </div>
          <div>
            <button
              style={btn}
              disabled={this.state.pending}
              type="button"
              onClick={() => this.register()}
            >
              Register
            </button>
          </div>
          {this.renderError()}
        </div>
      </div>
    );
  }
}

const styles = {
  icon: {
    fontSize: "2em",
    padding: "1em",
  },
  topSpacing: {
    height: "25vh",
  },
  header: {
    fontSize: "5em",
    marginTop: ".5em",
    marginBottom: ".5em",
  },
};
