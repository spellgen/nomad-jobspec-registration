# nomad-jobspec-registration

A little command line tool to register a service locally based on the information
found in a nomad jobspec.

We try to be clever about which interface to register, but if desired one can 
specify which with the environment variable "CONSUL_SERVICE_INTERFACE".
