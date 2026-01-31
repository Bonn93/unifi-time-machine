# UniFi Time-Machine

UniFi Time-Machine is a Go application that creates beautiful timelapse videos from your UniFi Protect cameras. It provides a web interface to view the latest snapshots, watch generated timelapses, and monitor the system status.

## Web Console
<img width="1912" height="1987" alt="image" src="https://github.com/user-attachments/assets/a5313036-24fd-41f3-bd46-eed4b5edbcfd" />


## Features

-   **Automatic Timelapse Generation**: Periodically generates timelapse videos from your UniFi Protect camera snapshots.
-   **Web Interface**: A simple, clean web UI to view the latest snapshots, watch timelapses, and check system status.
-   **Multi-Arch Support**: Docker images are available for both x86 (amd64) and ARM64 architectures.
-   **Configurable**: Most settings can be configured using environment variables.
-   **Efficient**: Uses a background worker to process jobs and a caching mechanism to keep the UI responsive.

## Getting Started

There are several ways to run UniFi Time-Machine. The easiest way is to use Docker.

### Versions
Versions are linked to Git Tags on this repo such as `v0.0.1` and pushed to `Dockerhub`. Other tags of interest;

- dev ( latest build on dev branches )
- latest ( builds off main branch )
- tags eg: `v1.2.3` ( recommended )

From the initial release, a security update was made to the container `user` which is now `appuser` with a UID/GID of `1000`, this may break your existing database on disk and require a `chown` to fix permissions. See release notes for details. 

### Running with Docker Compose (Recommended)

This is the recommended way to run the application, as it simplifies the management of the container and its configuration.

1.  **Create a `.env` file**: Modify the provided `.env` file and fill in the values for your environment.

    At a minimum, you must set `UFP_API_KEY`, `TARGET_CAMERA_ID`, `APP_KEY`, and `ADMIN_PASSWORD`.

    `UFP_API_KEY` is generated in your UI Console for the site. Integrations -> `New API Key`.

    Note the API calls used within only leverage the `Protect` API Endpoints on `/v1/cameras/{id}` and `/v1/cameras/{id}/snapshot`.

    `TARGET_CAMERA_ID` is found my accessing your UI console, selecting the camera -> Settings and refer to the URL in your browser such as `https://192.168.1.1/protect/dashboard/all/sidepanel/device/<YOUR_CAMERA_ID>/manage`

2.  **Start the container**:

    ```bash
    docker-compose up -d
    ```

3.  **Access the web UI**: The web UI will be available at `http://localhost:8000` (or the port you specified in `HTTP_PORT`). Login with `admin` as the user, and the password you defined in the startup variables. 

### Running with `start.sh`

The `start.sh` script is a convenient way to run the application with Docker without using Docker Compose.

1.  **Configure the script**: Open the `start.sh` script and edit the configuration variables at the top of the file.

2.  **Make the script executable**:

    ```bash
    chmod +x start.sh
    ```

3.  **Run the script**:

    ```bash
    ./start.sh
    ```

### Building the Docker Image

If you want to build the Docker image yourself, you can use the `build.sh` script. This script builds a multi-arch image for `linux/amd64` and `linux/arm64`.

1.  **Make the script executable**:

    ```bash
    chmod +x build.sh
    ```

2.  **Run the script**:

    ```bash
    ./build.sh [tag]
    ```

    The optional `tag` argument specifies the Docker image tag. If not provided, it defaults to `latest`.

### Building from Source

If you want to build the application from source, you'll need Go 1.22 or later installed.

1.  **Install dependencies**:

    ```bash
    go mod tidy
    ```

2.  **Build the binary**:

    ```bash
    go build -o unifi-time-machine ./cmd/server
    ```

3.  **Run the application**:

    ```bash
    ./unifi-time-machine
    ```

    You will need to set the required environment variables before running the application.

## Configuration

The application is configured using environment variables. See the `.env` file for a complete list of available options and their descriptions.

A new variable `SHARE_LINK_EXPIRY_HOURS` has been added to control the expiry of shared links. Default is `4` hours. Setting this to `0` or less will make links unlimited.

## Caveats
This is an early release, im still tweaking and deciding how ffmpeg, encoders etc will work as taking a lot of data in ie 365 days etc may have I/O and memory issues. It's highly likely that if the container is killed during encoding, you may corrupt the entire lapse file requiring you to leverage a older file, but have not tested this, nor documented recovery. I'd recommend regular backups, and having a stable environment with a UPS etc if you want to try and rely on this.

I have also not done any capacity planning or much storage optimisation. 

## Next Features
* GPU Support, perhaps on an Intel GPU because I've never done that.
* More than one camera in your Protect app?
* Cloud Backups & Tiered Storage for Edge/IOT deployments
* Public URL Sharing

## Contributing
I welcome any contributions or ideas.
