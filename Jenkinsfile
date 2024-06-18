node (agent){
    def app
    stage('Clone repository') {
        /* Let's make sure we have the repository cloned to our workspace */
        checkout scm
    }
    stage('Build 3270_io image') {
        /* This builds the actual 3270_io image; synonymous to
         * docker build on the command line */
        dir("app") {
            sh 'docker image build --no-cache -t 3270_io .'
        }
    }
    stage('Push 3270_io image') {
        /* Finally, we'll push the 3270_io image with two tags:
         * First, the incremental build number from Jenkins
         * Second, the 'latest' tag.
         * Pushing multiple tags is cheap, as all the layers are reused. */
        sh 'docker tag 3270_io:latest reg.jnnn.gs/3270_io:latest'
        sh 'docker login --username=sysad --password=sysad reg.jnnn.gs'
        sh 'docker push reg.jnnn.gs/3270_io:latest' 
    }
    stage('Prepare build script') {
        /* Ensure the build script is executable */
        sh 'chmod +x build.sh'
    }
    stage('Run build script') {
        /* This stage runs the build.sh script to build both Linux and Windows images */
        sh './build.sh'
    }
    stage('Push Linux and Windows images') {
        /* Pushing the Linux image */
        sh 'docker tag 3270connect-linux:latest reg.jnnn.gs/3270connect-linux:latest'
        sh 'docker push reg.jnnn.gs/3270connect-linux:latest'

        /* Pushing the Windows image */
        sh 'docker tag 3270connect-windows:latest reg.jnnn.gs/3270connect-windows:latest'
        sh 'docker push reg.jnnn.gs/3270connect-windows:latest'
    }
    stage('Test HTTP Request') {
        def response = httpRequest "http://httpbin.org/response-headers?param1=123"
        println("Status: ${response.status}")
        println("Response: ${response.content}")
        println("Headers: ${response.headers}")
    }
}
