import React from "react";
import {
  routeTo,
  hasUserInfo,
  getUserInfo,
  setUserInfo,
  request,
} from "../../util";
import { btn, input } from "../../styles/input";

export default class Login extends React.Component {
  constructor(props) {
    super(props);

    this.state = {
      oldPassword: "",
      newPassword: "",
      confirmPassword: "",
      error: "",
    };
  }

  async perform() {
    if (this.state.newPassword !== this.state.confirmPassword) {
      this.setState({ error: "Passwords do not match" });
      return;
    }

    const { email } = getUserInfo();
    const url = `user/change_password`;
    const params = {
      old_password: this.state.oldPassword,
      new_password: this.state.newPassword,
    };

    this.setState({pending:true,error:""});
    const { data, error } = await request(url, params);
    this.setState({pending:false});

    if (error) {
      this.setState({ error: error });
      return;
    }

    setUserInfo(email, data);
    return routeTo("/dashboards");
  }

  renderError() {
    return <div role="alert" style={styles.error}>{this.state.error}</div>;
  }

  render() {
    if (!hasUserInfo()) {
      routeTo("/");
      return <div></div>;
    }

    return (
      <div>
        <div style={styles.topSpacing}></div>
        <div style={styles.loginBox}>
          <div style={styles.icon}>Change Password</div>
          <div>
            <label style={{ display: "block" }}>
              <span className="sr-only">Old password</span>
              <input
                style={input}
                aria-label="old password"
                placeholder="old password"
                type="password"
                value={this.state.oldPassword}
                onChange={(e) => this.setState({ oldPassword: e.target.value })}
              />
            </label>
          </div>
          <div>
            <label style={{ display: "block" }}>
              <span className="sr-only">New password</span>
              <input
                style={input}
                aria-label="new password"
                placeholder="new password"
                type="password"
                value={this.state.newPassword}
                onChange={(e) => this.setState({ newPassword: e.target.value })}
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
                onChange={(e) =>
                  this.setState({ confirmPassword: e.target.value })
                }
              />
            </label>
          </div>
          <div>
            <button style={btn} disabled={this.state.pending} type="button" onClick={() => this.perform()}>
              Login
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
  loginBox: {
    margin: "auto",
    padding: "4px",
    border: "2px dashed",
    borderColor: "#0b0b0b",
    backgroundColor: "#1c1c1c",
    width: "min(90vw, 30em)",
    textAlign: "center",
  },
  error: {
    fontSize: "1.5em",
    color: "#ffb3b3",
  },
};
