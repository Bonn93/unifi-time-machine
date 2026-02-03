# UniFi Time-Machine

UniFi Time-Machine is a Go application that creates beautiful timelapse videos from your UniFi Protect cameras. It provides a web interface you can access directly, or behind a reverse proxy/load balancer.



## Web Console - Daily View
<img width="1663" height="1813" alt="image" src="https://github.com/user-attachments/assets/f8a84e85-a78e-41dd-857c-320df74924cc" />

## Web Console - Gallery
<img width="1651" height="996" alt="image" src="https://github.com/user-attachments/assets/e730967b-b32d-449b-ae96-f258d2a907e6" />

## Web Console - Admin Panel
<img width="1238" height="1038" alt="image" src="https://github.com/user-attachments/assets/17a73cef-4e40-44f3-93d3-e71541fb9322" />

## Web Console - Share Time Lapse


## Features

-   **Automatic Timelapse Generation**: Periodically generates timelapse videos from your UniFi Protect camera snapshots.
-   **Daily Gallery**: Takes hourly images of your target camera and builds a 24 hour gallery, sort and filter by date.
-   **Web Interface**: A simple, clean web UI to view the latest snapshots, watch timelapses, and check system status.
-   **Multi-Arch Support**: Docker images are available for both x86 (amd64) and ARM64 architectures.
-   **Configurable**: Most settings can be configured using environment variables. 
-   **Efficient**: Uses a background worker to process jobs and a caching mechanism to keep the UI responsive.

## Getting Started

There are several ways to run UniFi Time-Machine. The easiest way is to use Docker and highly recommended.

### Versions
Versions are linked to Git Tags on this repo such as `v0.0.1` and pushed to `Dockerhub`. Other tags of interest;

- dev ( latest build on dev branches )
- latest ( builds off main branch ) - will be deprecated in favour of git hash/sha256 builds.
- tags eg: `v1.2.3` ( recommended )

From the initial release, a security update was made to the container `user` which is now `appuser` with a UID/GID of `1000`, this may break your existing database on disk and require a `chown` to fix permissions. See release notes for details. 

If leveraging a docker bind mount or mapping, ensure you set this. `chown -R 1000:1000 data/` on the host directory!

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

1.  **Configure the script**: Open the `start.sh` script and edit the configuration variables at the top of the file. Note that variables like APP_KEY and UFP KEY are read from your environment. Pending you shell env, this is usually set via `export UFP_API_KEY` in files like your `~/.bash_profile` or `~/.bashrc`.

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

If you want to build the application from source, you'll need Go 1.25 or later installed. Using docker is recommended as we need CGO with SQLite and other dependencies that's much cleaner.

1.  **Install dependencies**:

    ```bash
    go mod tidy
    ```

2.  **Build the binary**:

    ```bash
    CGO_ENABLED=1 GOOS=$GOOS GOARCH=$GOARCH go build -ldflags '-s -w -extldflags "-static"' -tags osusergo,netgo -o /unifi-time-machine ./cmd/server
    ```

3.  **Run the application**:

    ```bash
    ./unifi-time-machine
    ```

    You will need to set the required environment variables before running the application. Web folder must be accessible etc. 

## Configuration

The application is configured using environment variables. See the `.env` file for a complete list of available options and their descriptions.

## Caveats
This project is still in its early days and bugs etc are expected alongside major changes. A 1.0.0 release would represent something of more mature stability after more field testing and feedback.



## Next Features
* GPU Support
* More than one camera in your Protect app?
* Cloud Backups & Tiered Storage for Edge/IOT deployments
* Public URL Sharing - Done!
* AI/Video Summary - summarise an uploaded video such as a mp4 from other systems and create a summary of detected objects, events etc
* Payment/Cashier tracker for retail environments based on payment terminal transactions outside of shopify for the rest of the world

## Contributing
I welcome any contributions or ideas.
