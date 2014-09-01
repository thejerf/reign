# Executive Summary

* Run `create_signing_cert` to produce `signing.key` and `signing.crt`.
* Run `create_node_cert` to produce `node.X.key` and `node.X.crt` for use
  with each node, by that ID.
  * This also produces a signing.crl file too. Keep this around, though
    it is not intrinsically sensitive itself.
* Each node needs `signing.crt` and its .key and .crt for itself.
  * DO NOT distribute `signing.key` to the nodes. Ideally, after creating
    the node certs, remove `signing.key` from the network entirely! This
    file is the key to the kingdom.
* Take care of these files!
* To know what magic these scripts are doing, read this file.

# Certificates for Clusters

To ensure maximum security for your cluster, reign uses TLS to ensure both
integrity and identity. We will end up with the following:

* A signing certificate, which consists of:
  * The signing certificate's private key
  * The certificate's public component.
* A certificate for each node that you may create, which has:
  * A private component
  * A public component

In this file, we will walk through how to use OpenSSL to create these
values from scratch, so that you know what is going on. However, the
provided shell scripts are OK to use (though bear in mind they produce
certificates not protected by passwords).

The setup we are about to create makes the private signing key the key to
the kingdom, and it should defended exactly as much as you care about your
cluster's security. It's probably a good idea to take it offline entirely.

# Creating the Signing Certificate

This creates the private key for the certificate. This is the Crown Jewel
of the collection of files we are creating.

This command will ask you for a password. You can either go ahead and use
that, or use the second command to strip it off that file. (If you lose it,
you will have to reconstruct the cluster keys from scratch.) Fill out the
certificate request as you see fit; none of these fields are used by reign.

    openssl req -newkey rsa:4096 -keyout signing.key -out signing.req

    # Optional: Remove the password
    cp signing.key signing.key.orig
    openssl rsa -in signing.key.orig -out signing.key

You'll find a lot of guides on the net use 1024 bits; this is perilously low
in the modern world. 2048 may be safe, but given that reign is virtually
guaranteed to be running on server-class hardware, I am personally going to
take the safe route out here and recommend 4096.

We now create the certificate from the request:

    openssl x509 -req -days 3650 -in signing.req -signkey signing.key -out signing.crt

You now have a signing.key and a signing.crt; you will not need the .req
(or the .orig if you stripped the password).

# Creating the Node Certificates

We now create the node certificates, and use the signing certificate to
sign them. This time, there IS a field that matters; the Common Name (CN)
field of the certificate must be the ID of the node that will use this key,
as just the number ("1", "32", etc).

    openssl req -newkey rsa:4096 -keyout node.1.key -out node.1.req

We then sign it:

    openssl x509 -req -days 3650 -in node.1.req -CA signing.crt -CAkey signing.key -out node.1.crt -CAcreateserial

This also creates a "signing.srl" file; you should keep that around, too.

To be accepted as a valid node, the node must present a certificate that is
signed with the cluster certificate, and has the node ID in the Common Node
that matches the Node ID it is claiming.

# Scripts

Two scripts are shipped with this distribution, which you can execute to
generate these keys. By default, they will fill in the certificate values
with vaguely useful values in most fields, and correctly fill in the common
name of the node certificates. The resulting keys will be unencrypted. The
two scripts are:

* create_signing_cert - creates the signing certificate according to
  these parameters
* create_node_cert - for each number passed in as a command line
  parameter, it will create a certificate for that node. Note this does
  not check for validity of the node ID.

The certificates and keys are created in the current working directory.