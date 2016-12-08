# <<<<<<< HEAD
# # All Vagrant configuration is done below. The "2" in Vagrant.configure
# # configures the configuration version (we support older styles for
# # backwards compatibility). Please don't change it unless you know what
# # you're doing.

# Vagrant.configure(2) do |config|
#   config.vm.define "DoC-nomad-vm"
#   # Use the ubuntu-14.04.3-cobaltbasedev image for development
#   config.vm.box = "ubuntu-14.04.3-cobaltbasedev"
#   config.vm.box_url = "https://registry.mo.sap.corp/api/vagrant/vagrant-dev-local/ubuntu-14.04.3-cobaltbasedev"
#   config.vbguest.auto_update = false
#   config.vm.box_download_insecure = true
#   config.vm.provider :virtualbox do |vb|
#     vb.customize ["modifyvm", :id, "--natdnshostresolver1", "on"]
#   end

#   # Sync up the project folder from laptop host
#   config.vm.synced_folder ".", "/home/vagrant/DoC", :mount_options => ["dmode=755,fmode=755"]

#   # In case the box file has been updated, we want to pick up the changes
#   config.vm.box_check_update = true

#   # Give a host name
#   config.vm.hostname = "DoC-host"
  
#   #this force vagrant to create eth1 interface that is used by docker to advertise overlay network.
#   config.vm.network "private_network", ip: "10.100.192.200"

#   # Configure network
#   ##Consul tunnel from laptop to vagrant vm @ http://localhost:8500/ui 
#   config.vm.network :forwarded_port, guest: 8500, guest_ip:"10.0.2.15", host: 8500, host_ip:"0.0.0.0" 

#   ##Metadata Service tunnel from laptop to vagrant vm @ http://localhost:8080/greeting
#   config.vm.network :forwarded_port, guest: 4000, guest_ip:"10.0.2.15", host: 4000, host_ip:"0.0.0.0" 

#   ##Nginx tunnel from latop to vagrant vm @ http://localhost:8090/greeting
#   config.vm.network :forwarded_port, guest: 3333, guest_ip:"10.0.2.15", host: 3333, host_ip:"0.0.0.0" 
#   # config.vm.network :forwarded_port, guest: 3001, guest_ip:"10.0.2.15", host: 3001, host_ip:"0.0.0.0" 

#   #create a pod overlay network
#   config.vm.provision :shell, inline: "docker network create -d overlay pod-network"

#   config.vm.provision "shell", privileged: false, inline: <<-EOF
#     echo "Vagrant Box provisioned!"
#     echo "Consul UI can be accessed at:  http://localhost:8500/ui"
#     echo "Nginx can be accessed at:  http://localhost:8090/health.txt"
#     echo "Metadata service can be accessed at:  http://localhost:9000/greeting"
#   EOF

# end
# =======
# -*- mode: ruby -*-
# vi: set ft=ruby :

GO_VERSION = "1.6.3"

GLIDE_VERSION = "0.11.1"

#GO_PACKAGE = "github.com/iverberk/nomad-ui"


unless Vagrant.has_plugin?("vagrant-vbguest")
  raise 'The following plugin is required: vagrant-vbguest.'\
        ' You can install it with \'vagrant plugin install vagrant-vbguest\''
end

Vagrant.configure(2) do |config|
  config.vm.box = "centos/7"
  config.vm.box_check_update = false
  # config.vbguest.auto_update = false
  config.vm.network "forwarded_port", guest: 3000, host: 3000, host_ip: "127.0.0.1"
  config.vm.network "forwarded_port", guest: 3333, host: 3333, host_ip: "127.0.0.1"

  config.vm.provision "shell", inline: <<-SHELL
    set -xe

    yum install -y epel-release
    yum install -y golang-#{GO_VERSION}
    yum install -y git-core

    curl -s https://rpm.nodesource.com/setup_4.x | sh -
    yum install -y nodejs
    yum install npm  
SHELL

  # config.vm.synced_folder ".", "/vagrant", type: "nfs"
  # config.vm.synced_folder ".", "/vagrant", :mount_options => ["dmode=755,fmode=755"]
  config.vm.synced_folder ".", "/home/vagrant/DoC", :mount_options => ["dmode=755,fmode=755"]
  config.vm.define "nomad-ui"
end

