/*
 * Copyright 2014-2016 the libsecp256k1 contributors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http:
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package org.bitcoin;

/**
 * This class holds the context reference used in native methods 
 * to handle ECDSA operations.
 */
public class Secp256k1Context {
  private static final boolean enabled;
  private static final long context;

  static {
      boolean isEnabled = true;
      long contextRef = -1;
      try {
          System.loadLibrary("secp256k1");
          contextRef = secp256k1_init_context();
      } catch (UnsatisfiedLinkError e) {
          System.out.println("UnsatisfiedLinkError: " + e.toString());
          isEnabled = false;
      }
      enabled = isEnabled;
      context = contextRef;
  }

  public static boolean isEnabled() {
     return enabled;
  }

  public static long getContext() {
     if(!enabled) return -1;
     return context;
  }

  private static native long secp256k1_init_context();
}
